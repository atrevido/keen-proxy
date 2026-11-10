package ndmhook

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

var quietLog = slog.New(slog.NewTextHandler(io.Discard, nil))

func fixture(t *testing.T) paths {
	t.Helper()
	dir, err := os.MkdirTemp("", "ndmh-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := paths{Core: filepath.Join(dir, "ndm.sock"), HookDir: filepath.Join(dir, "ndm", "netfilter.d"), StateDir: filepath.Join(dir, "run"), Binary: filepath.Join(dir, "absent-binary")}
	if err := os.Mkdir(filepath.Dir(p.HookDir), 0700); err != nil {
		t.Fatal(err)
	}
	core, err := net.ListenUnix("unix", &net.UnixAddr{Name: p.Core, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { core.Close() })
	return p
}

func runHook(t *testing.T, path, family, table string) {
	t.Helper()
	cmd := exec.Command("/bin/sh", path)
	// No executable lookup is available: the callback must use shell builtins.
	cmd.Env = []string{"PATH=/nonexistent", "type=" + family, "table=" + table}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hook failed: %v %s", err, out)
	}
}

func TestShellCallbackFiltersEventsWithoutHelper(t *testing.T) {
	for _, tc := range []struct {
		family, table string
		want          bool
	}{
		{"iptables", "nat", true}, {"iptables", "filter", true}, {"iptables", "mangle", true}, {"", "nat", true},
		{"ip6tables", "nat", false}, {"iptables", "raw", false}, {"iptables", "", false}, {"unexpected", "nat", false},
	} {
		t.Run(tc.family+"/"+tc.table, func(t *testing.T) {
			p := fixture(t)
			events := make(chan struct{}, 10)
			h, err := start(p, func() {
				select {
				case events <- struct{}{}:
				default:
				}
			}, quietLog)
			if err != nil || h == nil {
				t.Fatalf("start: %v", err)
			}
			defer h.Close()
			runHook(t, p.hook(), tc.family, tc.table)
			if tc.want {
				select {
				case <-events:
				case <-time.After(time.Second):
					t.Fatal("shell callback did not notify")
				}
			} else {
				select {
				case <-events:
					t.Fatal("irrelevant callback notified")
				case <-time.After(30 * time.Millisecond):
				}
			}
		})
	}
}

func TestHookCleanupAndQueuedCallback(t *testing.T) {
	p := fixture(t)
	h, err := start(p, func() {}, quietLog)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p.hook())
	if err != nil || info.Mode().Perm() != 0755 {
		t.Fatal("hook not executable")
	}
	script, err := os.ReadFile(p.hook())
	if err != nil {
		t.Fatal(err)
	}
	queued := filepath.Join(filepath.Dir(p.Core), "queued.sh")
	os.WriteFile(queued, script, 0700)
	h.Close()
	h.Close()
	runHook(t, queued, "iptables", "nat")
	for _, path := range []string{p.hook(), p.event()} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("owned artifact remains: %s", path)
		}
	}
}

func TestUnsupportedPlatformDoesNotCreateArtifacts(t *testing.T) {
	for _, kind := range []string{"core absent", "opkg absent", "core file"} {
		t.Run(kind, func(t *testing.T) {
			p := fixture(t)
			switch kind {
			case "core absent":
				p.Core += "-absent"
			case "opkg absent":
				p.HookDir = filepath.Join(filepath.Dir(p.HookDir), "absent", "netfilter.d")
			case "core file":
				p.Core += "-file"
				os.WriteFile(p.Core, nil, 0600)
			}
			h, err := start(p, func() { t.Error("unexpected event") }, quietLog)
			if h != nil || err != nil {
				t.Fatalf("unsupported platform: %v %v", h, err)
			}
			if _, err := os.Stat(p.StateDir); !os.IsNotExist(err) {
				t.Fatal("created runtime state on unsupported platform")
			}
		})
	}
	h, err := Start("/custom/config.yaml", func() { t.Error("unexpected event") }, quietLog)
	if h != nil || err != nil {
		t.Fatal("custom launch enabled hook")
	}
}

// OpenWrt may use the managed /opt layout without providing an NDM dispatcher.
// Exercise public startup as Linux/root so earlier launch gates cannot hide writes.
func TestManagedLinuxWithoutNDMDoesNotCreateArtifacts(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		t.Skip("requires Linux/root to exercise the complete managed launch path")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	previous := managed
	t.Cleanup(func() { managed = previous })
	for _, kind := range []string{"native OpenWrt", "OPKG directory without NDM", "stale regular socket path"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			managed = paths{
				Core: filepath.Join(root, "ndm.core.socket"), Binary: exe,
				HookDir:  filepath.Join(root, "opt/etc/ndm/netfilter.d"),
				StateDir: filepath.Join(root, "var/run/keenproxy/netfilter"),
			}
			if kind != "native OpenWrt" {
				if err := os.MkdirAll(filepath.Dir(managed.HookDir), 0755); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "stale regular socket path" {
				if err := os.WriteFile(managed.Core, []byte("not an NDM socket"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			h, err := Start("/opt/etc/keenproxy/config.yaml", func() { t.Error("unexpected NDM notification") }, quietLog)
			if err != nil || h != nil {
				t.Fatalf("non-NDM Linux enabled callbacks: hook=%v error=%v", h, err)
			}
			h.Close()
			for _, path := range []string{managed.HookDir, managed.StateDir} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("created hook artifacts on non-NDM Linux: %s (%v)", path, err)
				}
			}
		})
	}
}

func TestForeignArtifactsPreservedAndFailedSetupReleasesLock(t *testing.T) {
	for _, kind := range []string{"public state", "state symlink", "event file", "event symlink", "hook file", "hook symlink"} {
		t.Run(kind, func(t *testing.T) {
			p := fixture(t)
			os.MkdirAll(p.StateDir, 0700)
			os.MkdirAll(p.HookDir, 0700)
			conflict := ""
			switch kind {
			case "public state":
				os.Chmod(p.StateDir, 0755)
			case "state symlink":
				os.Remove(p.StateDir)
				os.Mkdir(p.StateDir+"-target", 0700)
				os.Symlink(p.StateDir+"-target", p.StateDir)
			case "event file":
				conflict = p.event()
				os.WriteFile(conflict, []byte("foreign"), 0600)
			case "event symlink":
				conflict = p.event()
				os.Symlink(p.Core, conflict)
			case "hook file":
				conflict = p.hook()
				os.WriteFile(conflict, []byte("#!/bin/sh\necho foreign\n"), 0755)
			case "hook symlink":
				conflict = p.hook()
				os.Symlink(p.Core, conflict)
			}
			h, err := start(p, func() {}, quietLog)
			if h != nil {
				h.Close()
			}
			if err == nil {
				t.Fatal("foreign artifact accepted")
			}
			if conflict != "" {
				if _, err := os.Lstat(conflict); err != nil {
					t.Fatal("foreign artifact removed")
				}
				os.Remove(conflict)
			}
			if kind == "state symlink" {
				os.Remove(p.StateDir)
				os.Mkdir(p.StateDir, 0700)
			}
			os.Chmod(p.StateDir, 0700)
			h, err = start(p, func() {}, quietLog)
			if err != nil || h == nil {
				t.Fatalf("setup leaked ownership: %v", err)
			}
			h.Close()
		})
	}
}

func TestStaleOwnedFilesAndOperatorReplacement(t *testing.T) {
	p := fixture(t)
	os.MkdirAll(p.StateDir, 0700)
	os.MkdirAll(p.HookDir, 0700)
	os.WriteFile(p.event(), []byte(eventValue), 0600)
	os.WriteFile(p.hook(), []byte(marker+"\nold owned script\n"), 0755)
	h, err := start(p, func() {}, quietLog)
	if err != nil {
		t.Fatal(err)
	}
	foreign := []byte("operator replacement")
	os.WriteFile(p.hook(), foreign, 0755)
	os.WriteFile(p.event(), foreign, 0600)
	h.Close()
	for _, path := range []string{p.hook(), p.event()} {
		body, err := os.ReadFile(path)
		if err != nil || string(body) != string(foreign) {
			t.Fatal("cleanup removed operator replacement")
		}
	}
}

func TestCloseKeepsOwnershipUntilCallbackCompletes(t *testing.T) {
	p := fixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	h, err := start(p, func() { once.Do(func() { close(entered) }); <-release }, quietLog)
	if err != nil {
		t.Fatal(err)
	}
	runHook(t, p.hook(), "iptables", "nat")
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("callback missing")
	}
	done := make(chan struct{})
	go func() { h.Close(); close(done) }()
	defer func() { close(release); <-done }()
	// Wait for watcher shutdown while callback completion still retains ownership.
	for {
		select {
		case _, ok := <-h.watcher.Events:
			if !ok {
				goto closed
			}
		case <-time.After(time.Second):
			t.Fatal("watcher did not close")
		}
	}
closed:
	next, err := start(p, func() {}, quietLog)
	if next != nil {
		next.Close()
	}
	if err == nil {
		t.Fatal("new instance stole ownership during Close")
	}
}

func TestHookKeepsOwnershipMarkerValidDuringCallbacks(t *testing.T) {
	p := fixture(t)
	invalid := make(chan struct{}, 1)
	h, err := start(p, func() {
		body, err := os.ReadFile(p.event())
		if err != nil || string(body) != eventValue {
			select {
			case invalid <- struct{}{}:
			default:
			}
		}
	}, quietLog)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	cmd := exec.Command("/bin/sh", "-c", `i=0; while [ "$i" -lt 2000 ]; do . "$1"; i=$((i+1)); done`, "sh", p.hook())
	cmd.Env = []string{"PATH=/nonexistent", "type=iptables", "table=nat"}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("callbacks: %v %s", err, out)
	}
	select {
	case <-invalid:
		t.Fatal("callback transiently corrupted ownership marker")
	default:
	}
}

func TestManagedLaunchPolicy(t *testing.T) {
	for _, kind := range []string{"supported", "other OS", "unprivileged", "custom config", "other executable", "missing binary", "symlink executable"} {
		t.Run(kind, func(t *testing.T) {
			p := fixture(t)
			os.WriteFile(p.Binary, []byte("fixture executable"), 0700)
			host := launch{OS: "linux", UID: 0, Config: "/opt/etc/keenproxy/config.yaml", Executable: p.Binary}
			switch kind {
			case "other OS":
				host.OS = "darwin"
			case "unprivileged":
				host.UID = 1000
			case "custom config":
				host.Config = "/custom/config.yaml"
			case "other executable":
				host.Executable = "/other/binary"
			case "missing binary":
				os.Remove(p.Binary)
			case "symlink executable":
				actual := p.Binary + "-actual"
				os.Rename(p.Binary, actual)
				os.Symlink(actual, p.Binary)
				host.Executable = actual
			}
			h, err := startForLaunch(p, host, func() {}, quietLog)
			if err != nil {
				t.Fatal(err)
			}
			want := kind == "supported" || kind == "symlink executable"
			if (h != nil) != want {
				t.Fatalf("managed policy %s: enabled=%v", kind, h != nil)
			}
			h.Close()
		})
	}
}

func TestHookIgnoresUnrelatedFilesystemEvents(t *testing.T) {
	p := fixture(t)
	events := make(chan struct{}, 10)
	h, err := start(p, func() { events <- struct{}{} }, quietLog)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	os.WriteFile(filepath.Join(p.StateDir, "unrelated"), []byte("other"), 0600)
	os.Chmod(p.event(), 0640)
	select {
	case <-events:
		t.Fatal("unrelated filesystem change requested repair")
	case <-time.After(50 * time.Millisecond):
	}
	runHook(t, p.hook(), "iptables", "nat")
	select {
	case <-events:
	case <-time.After(time.Second):
		t.Fatal("valid event lost after unrelated changes")
	}
}

func TestHookPreservesReplacedInodesOnClose(t *testing.T) {
	for _, kind := range []string{"same content", "symlink", "absent"} {
		t.Run(kind, func(t *testing.T) {
			p := fixture(t)
			h, err := start(p, func() {}, quietLog)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := os.ReadFile(p.hook())
			os.Rename(p.hook(), p.hook()+"-previous")
			switch kind {
			case "same content":
				os.WriteFile(p.hook(), body, 0755)
			case "symlink":
				os.Symlink(p.hook()+"-previous", p.hook())
			}
			h.Close()
			if kind != "absent" {
				if _, err := os.Lstat(p.hook()); err != nil {
					t.Fatal("replacement removed")
				}
			}
		})
	}
}

func TestHookContinuesAfterWatcherError(t *testing.T) {
	p := fixture(t)
	events := make(chan struct{}, 10)
	h, err := start(p, func() { events <- struct{}{} }, quietLog)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	h.watcher.Errors <- errors.New("synthetic event overflow")
	runHook(t, p.hook(), "iptables", "nat")
	select {
	case <-events:
	case <-time.After(time.Second):
		t.Fatal("watcher stopped after recoverable error")
	}
}

func TestHookInstallationFailureReleasesWatcher(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}
	p := fixture(t)
	os.Mkdir(p.HookDir, 0500)
	defer os.Chmod(p.HookDir, 0700)
	h, err := start(p, func() {}, quietLog)
	if h != nil || err == nil {
		t.Fatal("read-only hook directory accepted")
	}
	os.Chmod(p.HookDir, 0700)
	h, err = start(p, func() {}, quietLog)
	if h == nil || err != nil {
		t.Fatalf("setup failure retained watcher ownership: %v", err)
	}
	h.Close()
}

func TestHookPublicStartMatchesHost(t *testing.T) {
	p := fixture(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p.Binary = exe
	original := managed
	managed = p
	defer func() { managed = original }()
	h, err := Start("/opt/etc/keenproxy/config.yaml", func() {}, quietLog)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	want := runtime.GOOS == "linux" && os.Geteuid() == 0
	if (h != nil) != want {
		t.Fatalf("public startup enabled=%v, want %v", h != nil, want)
	}
}
