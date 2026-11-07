//go:build linux

package update

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEntwareUpdaterRequiresSupportedManagedPaths(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "bin/keenproxy")
	config := filepath.Join(root, "etc/keenproxy/config.yaml")
	service := filepath.Join(root, "etc/init.d/S99keenproxy")
	if err := os.MkdirAll(filepath.Dir(service), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(exe), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, nil, 0700); err != nil {
		t.Fatal(err)
	}
	if NewManaged(config, slog.Default()) != nil {
		t.Fatal("unmanaged binary exposed the Entware updater")
	}
	if newEntware(root, exe, config, "arm64", slog.Default()) != nil {
		t.Fatal("unmanaged installation exposed the updater")
	}
	if err := os.WriteFile(service, nil, 0700); err != nil {
		t.Fatal(err)
	}
	if newEntware(root, exe, config, "amd64", slog.Default()) != nil {
		t.Fatal("unsupported architecture exposed the updater")
	}
	m := newEntware(root, exe, config, "mipsle", slog.Default())
	if m == nil || m.Status().Available {
		t.Fatal("managed updater should wait for a release record")
	}
}

func TestDetachedInstallerKeepsLockAndPublishesResult(t *testing.T) {
	m := &Manager{dir: t.TempDir(), shell: "/bin/sh"}
	m.prefix = m.dir
	gate := filepath.Join(m.dir, "proceed")
	// Ensure an assertion failure cannot strand the fixture process.
	t.Cleanup(func() { _ = os.WriteFile(gate, nil, 0600) })
	script := []byte(`
[ "$*" = '--version v0.4.0 --expect-current v0.3.0 --without-trusttunnel --lite' ] || exit 1
[ ! -e /proc/self/fd/3 ] || exit 1
for try in 1 2 3 4 5 6 7 8 9 10; do
    [ ! -f "$KEENPROXY_PREFIX/proceed" ] || exit 0
    sleep 1
done
exit 1
`)
	current := installState{Current: "v0.3.0", Variant: "lite"}
	if err := m.startWorker(current, "v0.4.0", script); err != nil {
		t.Fatal(err)
	}
	restarted := &Manager{dir: m.dir}
	if job := restarted.job(); job == nil || job.State != "running" {
		t.Fatalf("restarted API lost the active job: %+v", job)
	}
	if err := m.startWorker(current, "v0.4.0", script); !errors.Is(err, ErrBusy) {
		t.Fatalf("second install should be blocked, got %v", err)
	}
	if err := os.WriteFile(gate, nil, 0600); err != nil {
		t.Fatal(err)
	}
	awaitWorkerResult(t, restarted, "success", "v0.4.0")

	if err := m.startWorker(current, "v0.5.0", []byte("exit 1\n")); err != nil {
		t.Fatal(err)
	}
	awaitWorkerResult(t, restarted, "failed", "v0.5.0")
}

func awaitWorkerResult(t *testing.T, m *Manager, want, target string) {
	t.Helper()
	until := time.Now().Add(4 * time.Second)
	for time.Now().Before(until) {
		job := m.job()
		if job != nil && job.State == want && job.Target == target {
			// The status write precedes the outer shell closing its lock.
			if f, err := m.lock(); err == nil {
				f.Close()
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	log, _ := os.ReadFile(filepath.Join(m.dir, "installer.log"))
	t.Fatalf("worker result = %+v, want %s for %s; installer log: %s", m.job(), want, target, log)
}
