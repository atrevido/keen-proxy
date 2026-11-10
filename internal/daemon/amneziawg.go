package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"time"

	"github.com/ordo-systems/keen-proxy/internal/amneziawg"
	"github.com/ordo-systems/keen-proxy/internal/catalog"
	"github.com/ordo-systems/keen-proxy/internal/compile"
	"github.com/ordo-systems/keen-proxy/internal/model"
	"github.com/ordo-systems/keen-proxy/internal/supervise"
	"golang.org/x/net/proxy"
)

const awgBinary = "/opt/bin/keenproxy-awg"

// awgProcess owns one sidecar and its private configuration file.
type awgProcess struct {
	client compile.AmneziaWGClient
	cancel context.CancelFunc
	done   chan struct{}
	path   string
}

// startConfigLocked tries the pending draft once the service is already
// restarting. The previous startup file remains authoritative on failure.
func (d *Daemon) startConfigLocked(ctx context.Context, active *model.Config) (*model.Config, error) {
	pending, err := model.Load(d.awgPendingPath())
	if errors.Is(err, os.ErrNotExist) {
		return active, d.applyLocked(ctx, active)
	}
	if err != nil || !reflect.DeepEqual(active.API, pending.API) {
		d.log.Warn("staged AmneziaWG configuration was not loaded")
		return active, d.applyLocked(ctx, active)
	}
	d.pendingAWG = pending
	catalog.AutoUpdate(pending)
	if err := d.checkAWGClients(ctx, pending); err != nil {
		d.log.Error("staged AmneziaWG configuration failed validation", "err", err)
		return active, d.applyLocked(ctx, active)
	}
	d.reloadGeoLocked(pending)
	d.neighbors = d.readNeighbors(pending)
	err = d.applyLocked(ctx, pending)
	if err == nil {
		err = d.saveAWGConfig(pending)
	}
	if err != nil {
		d.log.Error("staged AmneziaWG startup failed, restoring active configuration", "err", err)
		d.reloadGeoLocked(active)
		d.neighbors = d.readNeighbors(active)
		return active, d.applyLocked(ctx, active)
	}
	if err := d.clearPendingAWG(); err != nil {
		d.log.Error("AmneziaWG pending configuration was not removed", "err", err)
	}
	return pending, nil
}

// checkAWGClients checks the installed executable before applying staged routes.
func (d *Daemon) checkAWGClients(ctx context.Context, cfg *model.Config) error {
	for _, cn := range cfg.Connections {
		if cn.AmneziaWG != nil {
			listen := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(cn.AmneziaWG.SocksPort)))
			listener, err := net.Listen("tcp", listen)
			if err != nil {
				return fmt.Errorf("AmneziaWG client %q cannot use its SOCKS port: %w", cn.ID, err)
			}
			_ = listener.Close()
			if err := d.runAWGCheck(ctx, cn.AmneziaWG.Profile, "-check"); err != nil {
				return fmt.Errorf("AmneziaWG client %q did not pass startup validation: %w", cn.ID, err)
			}
		}
	}
	return nil
}

func (d *Daemon) awgPendingPath() string { return d.opt.ConfigPath + ".pending-awg" }

// EditableConfig returns the persisted draft when activation is pending.
func (d *Daemon) EditableConfig() *model.Config {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.pendingAWG != nil {
		return d.pendingAWG.Clone()
	}
	if d.cfg != nil {
		return d.cfg.Clone()
	}
	return nil
}

func hasAWG(cfg *model.Config) bool {
	if cfg != nil {
		for _, c := range cfg.Connections {
			if c.Type == model.ConnAmneziaWG {
				return true
			}
		}
	}
	return false
}

// awgProfileOnly ignores AWG profile text and management-only settings.
func awgProfileOnly(before, after *model.Config) bool {
	if before == nil {
		return false
	}
	a, b := before.Clone(), after.Clone()
	a.API = b.API
	for i := range a.Connections {
		if awg := a.Connections[i].AmneziaWG; awg != nil {
			awg.Profile = ""
		}
	}
	for i := range b.Connections {
		if awg := b.Connections[i].AmneziaWG; awg != nil {
			awg.Profile = ""
		}
	}
	return sameConfig(a, b)
}

// applyAWGLocked handles AWG-bearing drafts without reloading the running engine.
func (d *Daemon) applyAWGLocked(cfg *model.Config, out compile.Output) error {
	if sameConfig(d.cfg, cfg) {
		if err := d.clearPendingAWG(); err != nil {
			return err
		}
		return nil
	}
	if !awgProfileOnly(d.cfg, cfg) || d.pendingAWG != nil {
		if !reflect.DeepEqual(d.cfg.API, cfg.API) {
			return errors.New("apply API listener changes separately from a staged AmneziaWG configuration")
		}
		raw, err := cfg.Marshal()
		if err != nil {
			return err
		}
		if err := writeFile(d.awgPendingPath(), raw, 0o600); err != nil {
			return fmt.Errorf("stage AmneziaWG configuration: %w", err)
		}
		d.pendingAWG = cfg.Clone()
		d.log.Info("AmneziaWG configuration staged for next service start")
		return nil
	}
	prepared, err := d.prepareAWG(out.AmneziaWG)
	if err != nil {
		return err
	}
	if err := d.saveAWGConfig(cfg); err != nil {
		for _, p := range prepared {
			_ = os.Remove(p.path)
		}
		return err
	}
	d.replaceAWG(out.AmneziaWG, prepared)
	d.cfg = cfg
	d.live.Store(cfg)
	d.serverHosts = out.ServerHosts
	for id := range prepared {
		delete(d.health, id)
	}
	return nil
}

// sameConfig ignores nil versus empty collections produced by the panel.
func sameConfig(a, b *model.Config) bool {
	x, err := a.Marshal()
	y, otherErr := b.Marshal()
	return err == nil && otherErr == nil && bytes.Equal(x, y)
}

func (d *Daemon) clearPendingAWG() error {
	if err := os.Remove(d.awgPendingPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	d.pendingAWG = nil
	return nil
}

func (d *Daemon) saveAWGConfig(cfg *model.Config) error {
	raw, err := cfg.Marshal()
	if err != nil {
		return err
	}
	old, err := os.ReadFile(d.opt.ConfigPath)
	if err != nil {
		return err
	}
	if err := writeFile(d.opt.ConfigPath+".previous", old, 0o600); err != nil {
		return err
	}
	return writeFile(d.opt.ConfigPath, raw, 0o600)
}

// prepareAWG writes all changed profiles before any process is stopped.
func (d *Daemon) prepareAWG(clients []compile.AmneziaWGClient) (map[string]*awgProcess, error) {
	prepared := map[string]*awgProcess{}
	for _, client := range clients {
		if existing := d.awg[client.ID]; existing != nil && existing.client == client {
			continue
		}
		path, err := d.writeAWGProfile(client.Profile)
		if err != nil {
			for _, p := range prepared {
				_ = os.Remove(p.path)
			}
			return nil, err
		}
		prepared[client.ID] = &awgProcess{client: client, path: path}
	}
	return prepared, nil
}

func (d *Daemon) writeAWGProfile(profile string) (string, error) {
	dir := filepath.Join(filepath.Dir(d.opt.RuleSetDir), "amneziawg")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "profile-*.conf")
	if err != nil {
		return "", err
	}
	_, err = f.WriteString(profile)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func (d *Daemon) replaceAWG(clients []compile.AmneziaWGClient, prepared map[string]*awgProcess) {
	next := make(map[string]*awgProcess, len(clients))
	for _, c := range clients {
		if p := d.awg[c.ID]; p != nil && p.client == c {
			next[c.ID] = p
		}
	}
	for id, p := range d.awg {
		if next[id] != p {
			p.cancel()
			<-p.done
			_ = os.Remove(p.path)
		}
	}
	for id, p := range prepared {
		ctx, cancel := context.WithCancel(context.Background())
		p.cancel, p.done = cancel, make(chan struct{})
		socks := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(p.client.SocksPort)))
		proc := &supervise.Process{Name: "amneziawg/" + id, Path: d.opt.AWGBinary, Args: []string{"-config", p.path, "-socks-listen", socks}, Env: append(os.Environ(), "KEENPROXY_AWG_SOCKS_PASSWORD="+d.awgPassword), Log: d.log}
		go func() { defer close(p.done); proc.Run(ctx) }()
		next[id] = p
	}
	d.awg = next
}

// ProbeAmneziaWG checks a draft without applying routes or starting a second
// instance of an already active peer identity.
func (d *Daemon) ProbeAmneziaWG(ctx context.Context, raw string) error {
	profile, err := amneziawg.Parse(raw)
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cfg != nil && d.cfg.UsesWireGuardIdentity(profile.ClientID) {
		return errors.New("the AmneziaWG profile reuses an active WireGuard client identity")
	}
	for _, p := range d.awg {
		if p.client.ClientID != profile.ClientID {
			continue
		}
		if p.client.Profile != profile.Config {
			return errors.New("this AmneziaWG identity is active; the edited profile cannot be tested concurrently")
		}
		return probeAWGSOCKS(ctx, p.client.SocksPort, d.awgPassword)
	}
	if err := d.runAWGCheck(ctx, profile.Config, "-probe"); err != nil {
		return fmt.Errorf("AmneziaWG HTTPS probe failed: %w", err)
	}
	return nil
}

func (d *Daemon) runAWGCheck(ctx context.Context, profile, option string) error {
	path, err := d.writeAWGProfile(profile)
	if err != nil {
		return err
	}
	defer os.Remove(path)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, d.opt.AWGBinary, "-config", path, option)
	if _, err := os.Stat("/opt/etc/ssl/certs/ca-certificates.crt"); err == nil {
		cmd.Env = append(os.Environ(), "SSL_CERT_FILE=/opt/etc/ssl/certs/ca-certificates.crt")
	}
	return cmd.Run()
}

// probeAWGSOCKS uses the running client to avoid moving an existing peer identity.
func probeAWGSOCKS(ctx context.Context, port uint16, password string) error {
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port)))
	dialer, err := proxy.SOCKS5("tcp", address, &proxy.Auth{User: "keenproxy", Password: password}, &net.Dialer{Timeout: 5 * time.Second})
	if err != nil {
		return err
	}
	transport := &http.Transport{DialContext: dialer.(proxy.ContextDialer).DialContext, DisableKeepAlives: true}
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://cp.cloudflare.com/generate_204", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("AmneziaWG HTTPS probe failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("AmneziaWG HTTPS probe returned status %d", resp.StatusCode)
	}
	return nil
}
