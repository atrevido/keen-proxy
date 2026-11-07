//go:build linux

package update

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/ordo-systems/keen-proxy/tools"
	"golang.org/x/sys/unix"
)

// NewManaged enables updates only for the stock root-owned Entware entry point.
func NewManaged(config string, log *slog.Logger) *Manager {
	exe, err := os.Executable()
	if err != nil || os.Geteuid() != 0 {
		return nil
	}
	return newEntware("/opt", exe, config, runtime.GOARCH, log)
}

func newEntware(prefix, exe, config, arch string, log *slog.Logger) *Manager {
	if !managedPaths(prefix, exe, config) || (arch != "arm64" && arch != "mipsle") {
		return nil
	}
	dir := filepath.Join(prefix, "var/lib/keenproxy/updates")
	if err := os.MkdirAll(dir, 0700); err != nil {
		log.Warn("could not prepare update state", "err", err)
		return nil
	}
	m := &Manager{
		metadata: filepath.Join(prefix, "share/keenproxy/release"), dir: dir, arch: arch, log: log,
		prefix: prefix, shell: filepath.Join(prefix, "bin/sh"),
		client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}
	m.start = m.launch
	return m
}

func managedPaths(prefix, exe, config string) bool {
	if config != filepath.Join(prefix, "etc/keenproxy/config.yaml") {
		return false
	}
	expected, err := filepath.EvalSymlinks(filepath.Join(prefix, "bin/keenproxy"))
	if err != nil || exe != expected {
		return false
	}
	info, err := os.Lstat(filepath.Join(prefix, "etc/init.d/S99keenproxy"))
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0100 == 0 {
		return false
	}
	return true
}

func (m *Manager) lock() (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(m.dir, "job.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrBusy
		}
		return nil, err
	}
	return f, nil
}

func (m *Manager) job() *Job {
	// If we can take the lock, no installer can change the state while we read it.
	f, lockErr := m.lock()
	if f != nil {
		defer f.Close()
	}
	b, err := os.ReadFile(filepath.Join(m.dir, "job"))
	if err != nil {
		return nil
	}
	state, target, ok := strings.Cut(strings.TrimSpace(string(b)), " ")
	if !ok || !stable.MatchString(target) {
		return nil
	}
	switch state {
	case "running":
		if !errors.Is(lockErr, ErrBusy) {
			state = "failed"
		}
	case "success", "failed":
	default:
		return nil
	}
	return &Job{State: state, Target: target}
}

// The outer shell retains the lock while its installer child closes descriptor
// three. The new daemon must never inherit the lock from the installer.
const worker = `
umask 077
shell=$1
job=$2
target=$3
shift 3
if "$shell" -s -- "$@" 3>&-; then
    result=success
else
    result=failed
fi
tmp="$job.$$"
printf '%s %s\n' "$result" "$target" > "$tmp" && mv -f "$tmp" "$job"
`

func (m *Manager) launch(current installState, target string) error {
	return m.startWorker(current, target, tools.Bootstrap)
}

func (m *Manager) startWorker(current installState, target string, installer []byte) error {
	lock, err := m.lock()
	if err != nil {
		return err
	}
	defer lock.Close()
	f, err := installerInput(m.dir, installer)
	if err != nil {
		return err
	}
	defer f.Close()
	log, err := os.OpenFile(filepath.Join(m.dir, "installer.log"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	args := []string{"-c", worker, "keenproxy-update", m.shell, filepath.Join(m.dir, "job"), target,
		"--version", target, "--expect-current", current.Current, "--without-trusttunnel"}
	if current.Variant == "lite" {
		args = append(args, "--lite")
	}
	cmd := exec.Command(m.shell, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = f, log, log
	cmd.ExtraFiles = []*os.File{lock}
	cmd.Env = append(os.Environ(), "KEENPROXY_PREFIX="+m.prefix)

	if err := writePrivate(m.dir, "job", []byte("running "+target+"\n")); err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		_ = writePrivate(m.dir, "job", []byte("failed "+target+"\n"))
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func installerInput(dir string, script []byte) (*os.File, error) {
	f, err := os.CreateTemp(dir, ".installer-")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(script); err != nil {
		f.Close()
		return nil, err
	}
	if _, err := f.Seek(0, 0); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
