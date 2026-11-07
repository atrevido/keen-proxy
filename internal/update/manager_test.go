package update

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type releaseTransport func(*http.Request) (*http.Response, error)

func (f releaseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fixtureManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	metadata := filepath.Join(dir, "release")
	if err := os.WriteFile(metadata, []byte("current v0.4.0\nprevious v0.3.0\nvariant full\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return &Manager{metadata: metadata, dir: dir, log: slog.Default(), arch: "arm64"}
}

func TestScheduledInstallRefreshesReleaseAndPersistsAttempt(t *testing.T) {
	m := fixtureManager(t)
	now := time.Date(2026, 11, 7, 4, 30, 0, 0, time.Local)
	var checks, starts int
	m.client = &http.Client{Transport: releaseTransport(func(r *http.Request) (*http.Response, error) {
		checks++
		if r.URL.String() != latestReleaseURL {
			t.Fatalf("unexpected release source: %s", r.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(
			`{"tag_name":"v0.5.0","assets":[{"name":"keenproxy-linux-arm64","state":"uploaded"},` +
				`{"name":"S99keenproxy","state":"uploaded"},{"name":"install.sh","state":"uploaded"},{"name":"SHA256SUMS","state":"uploaded"}]}`))}, nil
	})}
	m.start = func(current installState, target string) error {
		starts++
		if current.Current != "v0.4.0" || target != "v0.5.0" {
			t.Fatalf("unexpected scheduled transition: %+v to %s", current, target)
		}
		return nil
	}
	if p := m.Status().Preferences; p.Enabled || p.Time != defaultTime {
		t.Fatalf("unsafe defaults: %+v", p)
	}
	if err := m.SetPreferences(Preferences{Enabled: true, Time: "4:30"}); !errors.Is(err, ErrPreferences) {
		t.Fatalf("accepted invalid time: %v", err)
	}
	if err := m.SetPreferences(Preferences{Enabled: true, Time: "04:30"}); err != nil {
		t.Fatal(err)
	}
	m.scheduled(context.Background(), now.Add(-time.Minute))
	m.scheduled(context.Background(), now)
	m.scheduled(context.Background(), now.Add(30*time.Second))
	if checks != 1 || starts != 1 {
		t.Fatalf("checks=%d starts=%d, want one each", checks, starts)
	}
	if status := m.Status(); !status.Available || status.Latest != "v0.5.0" || status.CheckFailed {
		t.Fatalf("wrong availability: %+v", status)
	}
	if s, err := m.settings(); err != nil || s.AttemptDate != now.Format(time.DateOnly) {
		t.Fatalf("attempt was not persisted: %+v, %v", s, err)
	}
}

func TestRollbackDisablesAutoInstallOnlyWhenAccepted(t *testing.T) {
	m := fixtureManager(t)
	prefs := Preferences{Enabled: true, Time: "04:30"}
	if err := m.SetPreferences(prefs); err != nil {
		t.Fatal(err)
	}
	if err := m.Start("v0.2.0", true); !errors.Is(err, ErrTarget) {
		t.Fatalf("accepted a stale rollback target: %v", err)
	}
	m.start = func(installState, string) error { return ErrBusy }
	if err := m.Start("v0.3.0", true); !errors.Is(err, ErrBusy) {
		t.Fatalf("expected busy: %v", err)
	}
	if m.Status().Preferences != prefs {
		t.Fatal("failed launch changed preferences")
	}
	m.start = func(current installState, target string) error {
		if current.Current != "v0.4.0" || target != "v0.3.0" {
			t.Fatalf("unexpected rollback: %+v to %s", current, target)
		}
		return nil
	}
	if err := m.Start("v0.3.0", true); err != nil {
		t.Fatal(err)
	}
	if m.Status().Preferences.Enabled {
		t.Fatal("rollback left automatic installation enabled")
	}
}

func TestSchedulerStopsWhenServiceStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := &Manager{metadata: filepath.Join(t.TempDir(), "missing")}
	done := make(chan struct{})
	go func() {
		m.Run(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("update scheduler outlived its service")
	}
}

func TestReleaseCheckRejectsMalformedAndIncompleteResponses(t *testing.T) {
	for _, body := range []string{"{", `{"tag_name":"v0.5.0","assets":[]}`, `{"tag_name":"v0.5.0","draft":true}`} {
		if _, err := decodeRelease(strings.NewReader(body), "full", "arm64"); err == nil {
			t.Errorf("accepted unavailable release response %q", body)
		}
	}
}

func TestReleaseMustBeStableAndComplete(t *testing.T) {
	assets := []string{"keenproxy-linux-arm64", "install.sh", "S99keenproxy", "SHA256SUMS"}
	if !releaseReady("v0.4.0", assets, "full", "arm64") {
		t.Fatal("complete stable release was rejected")
	}
	if releaseReady("v0.4.0-rc.1", assets, "full", "arm64") ||
		releaseReady("v0.4.0", assets[:3], "full", "arm64") ||
		releaseReady("v0.4.0", assets, "lite", "arm64") {
		t.Fatal("pre-release, incomplete release or wrong variant accepted")
	}
	if !newer("v0.10.0", "v0.9.9") || newer("v0.3.0", "v0.3.0") || newer("v0.3.0", "v0.4.0") {
		t.Fatal("release ordering is incorrect")
	}
	if newer("v0.5.0-rc.1", "v0.4.0") || newer("v0.5.0", "v0.4.0-rc.1") {
		t.Fatal("an unstable version entered the update comparison")
	}
}

func TestScheduleUsesLocalMinuteAndOnlyOneAttempt(t *testing.T) {
	now := time.Date(2026, 11, 7, 4, 30, 12, 0, time.FixedZone("router", 3*3600))
	prefs := Preferences{Enabled: true, Time: "04:30"}
	if !due(prefs, "", now) || due(prefs, "2026-11-07", now) || due(prefs, "", now.Add(time.Minute)) ||
		due(Preferences{Time: "04:30"}, "", now) {
		t.Fatal("schedule ignored the local minute, previous attempt, or disabled setting")
	}
}

func TestParseInstallMetadata(t *testing.T) {
	state := readInstall(strings.NewReader("current v0.4.0\nprevious v0.3.0\nvariant lite\n"))
	if state.Current != "v0.4.0" || state.Previous != "v0.3.0" || state.Variant != "lite" {
		t.Fatalf("unexpected install state: %+v", state)
	}
	if state := readInstall(strings.NewReader("current ../../unsafe\nvariant full\n")); state.Current != "" {
		t.Fatal("accepted invalid installed tag")
	}
}
