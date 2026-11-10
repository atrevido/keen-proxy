// Package ndmhook bridges optional NDM netfilter callbacks to a running daemon.
// Detection or setup failure never disables the portable polling watchdog.
package ndmhook

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/sys/unix"
)

const marker = "#!/bin/sh\n# keenproxy-netfilter-hook-v1"
const eventValue = "keenproxy-netfilter-event-v1\n"

// paths separates host capability detection from the production installation.
type paths struct{ Core, HookDir, StateDir, Binary, Config string }

func managedPaths() paths {
	return paths{Core: "/var/run/ndm.core.socket", HookDir: "/opt/etc/ndm/netfilter.d", StateDir: "/var/run/keenproxy/netfilter", Binary: "/opt/bin/keenproxy", Config: "/opt/etc/keenproxy/config.yaml"}
}

func (p paths) event() string { return filepath.Join(p.StateDir, "changed") }
func (p paths) hook() string  { return filepath.Join(p.HookDir, "99-keenproxy.sh") }

// Start enables callbacks only on a managed Linux instance with NDM present.
// Unsupported platforms return nil; setup errors leave polling as the fallback.
// The caller owns its lifetime and must Close it after daemon shutdown, including error exits.
func Start(config string, onChange func(), log *slog.Logger) (*Hook, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return startForLaunch(managedPaths(), launch{runtime.GOOS, os.Geteuid(), config, exe}, onChange, log)
}

// launch records the process facts used by the optional-platform policy.
type launch struct {
	OS                 string
	UID                int
	Config, Executable string
}

func startForLaunch(p paths, host launch, onChange func(), log *slog.Logger) (*Hook, error) {
	if host.OS != "linux" || host.UID != 0 || !sameConfig(host.Config, p.Config) {
		return nil, nil
	}
	expected, err := filepath.EvalSymlinks(p.Binary)
	if err != nil {
		return nil, nil
	}
	if host.Executable != expected {
		return nil, nil
	}
	return start(p, onChange, log)
}

// sameConfig accepts path aliases only when they identify the managed config file.
func sameConfig(actual, expected string) bool {
	a, err := os.Stat(actual)
	if err != nil {
		return false
	}
	b, err := os.Stat(expected)
	return err == nil && os.SameFile(a, b)
}

// Hook holds exclusive ownership until its watcher and owned files are removed.
type Hook struct {
	once      sync.Once
	watcher   *fsnotify.Watcher
	done      chan struct{}
	path      paths
	script    []byte
	eventInfo os.FileInfo
	hookInfo  os.FileInfo
	lock      *os.File
}

func start(p paths, onChange func(), log *slog.Logger) (*Hook, error) {
	if !capable(p) {
		return nil, nil
	}
	lock, err := lockState(p.StateDir)
	if err != nil {
		return nil, err
	}
	keepLock := false
	defer func() {
		if !keepLock {
			lock.Close()
		}
	}()
	if err := checkHook(p.hook()); err != nil {
		return nil, err
	}
	if err := prepareEvent(p.event()); err != nil {
		return nil, err
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := watcher.Add(p.StateDir); err != nil {
		watcher.Close()
		return nil, err
	}
	h := &Hook{watcher: watcher, done: make(chan struct{}), path: p, script: hookScript(p.event()), lock: lock}
	keepLock = true
	h.eventInfo, _ = os.Lstat(p.event())
	go h.receive(onChange, log)
	if err := installHook(p.hook(), h.script); err != nil {
		h.Close()
		return nil, err
	}
	h.hookInfo, _ = os.Lstat(p.hook())
	return h, nil
}

// lockState serializes installation and cleanup for the full watcher lifetime.
// Its inode remains in place across restarts so competing instances share a lock.
func lockState(dir string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("firewall notification directory must be private")
	}
	f, err := os.OpenFile(filepath.Join(dir, "owner.lock"), os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func capable(p paths) bool {
	core, err := os.Stat(p.Core)
	if err != nil || core.Mode()&os.ModeSocket == 0 {
		return false
	}
	dir, err := os.Stat(filepath.Dir(p.HookDir))
	return err == nil && dir.IsDir()
}

func checkHook(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to replace foreign netfilter hook")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.HasPrefix(body, []byte(marker+"\n")) {
		return fmt.Errorf("refusing to replace foreign netfilter hook")
	}
	return nil
}

// prepareEvent accepts a stale owned marker but never truncates a foreign file.
func prepareEvent(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err == nil {
		defer f.Close()
		_, err = f.WriteString(eventValue)
		return err
	}
	if !os.IsExist(err) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to replace foreign event file")
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != eventValue {
		return fmt.Errorf("refusing to replace foreign event file")
	}
	return nil
}

func hookScript(event string) []byte {
	// Only shell builtins are needed. A queued callback after stop is harmless.
	quoted := "'" + strings.ReplaceAll(event, "'", "'\"'\"'") + "'"
	return []byte(marker + "\n" + `case "${type:-iptables}:$table" in
    iptables:nat|iptables:filter|iptables:mangle) ;;
    *) exit 0 ;;
esac
` + "event=" + quoted + "\n" + `[ -f "$event" ] && [ ! -L "$event" ] || exit 0
printf 'keenproxy-netfilter-event-v1\n' 1<> "$event" 2>/dev/null || :
`)
}

func installHook(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".keenproxy-hook-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = f.Write(body); err != nil {
		return err
	}
	if err = f.Chmod(0755); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func (h *Hook) receive(onChange func(), log *slog.Logger) {
	defer close(h.done)
	for {
		select {
		case e, ok := <-h.watcher.Events:
			if !ok {
				return
			}
			if e.Name == h.path.event() && e.Op&fsnotify.Write != 0 {
				onChange()
			}
		case err, ok := <-h.watcher.Errors:
			if !ok {
				return
			}
			log.Warn("netfilter event watcher error; polling remains active", "err", err)
		}
	}
}

// Close leaves operator replacements untouched and releases the lifetime lock last.
func (h *Hook) Close() error {
	if h == nil {
		return nil
	}
	h.once.Do(func() {
		defer h.lock.Close()
		h.watcher.Close()
		<-h.done
		removeOwned(h.path.hook(), h.hookInfo, h.script)
		removeOwned(h.path.event(), h.eventInfo, []byte(eventValue))
	})
	return nil
}

func removeOwned(path string, owned os.FileInfo, expected []byte) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(info, owned) {
		return
	}
	if body, err := os.ReadFile(path); err == nil && bytes.Equal(body, expected) {
		os.Remove(path)
	}
}
