// Command keenproxy runs the router daemon with an embedded sing-box engine.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/ordo-systems/keen-proxy/internal/api"
	"github.com/ordo-systems/keen-proxy/internal/compile"
	"github.com/ordo-systems/keen-proxy/internal/daemon"
	"github.com/ordo-systems/keen-proxy/internal/devices"
	"github.com/ordo-systems/keen-proxy/internal/engine"
	"github.com/ordo-systems/keen-proxy/internal/journal"
	"github.com/ordo-systems/keen-proxy/internal/model"
	"github.com/ordo-systems/keen-proxy/internal/netfilter"
	"github.com/ordo-systems/keen-proxy/web"
)

// main selects dry-run, synthetic demo, or the live router lifecycle.
func main() {
	configPath := flag.String("config", "/opt/etc/keenproxy/config.yaml", "configuration file path")
	arpPath := flag.String("arp", "/proc/net/arp", "ARP table path")
	ruleSetDir := flag.String("rule-sets", "/opt/var/lib/keenproxy/rule-sets", "derived rule-set directory")
	watch := flag.Duration("watch", 30*time.Second, "watchdog interval for firewall and device discovery")
	dryRun := flag.Bool("dry-run", false, "print derived engine/firewall settings without applying them")
	iptables := flag.String("iptables", "/opt/sbin/iptables", "Entware iptables executable")
	restore := flag.String("iptables-restore", "/opt/sbin/iptables-restore", "Entware iptables-restore executable")
	uiDemo := flag.Bool("ui-demo", false, "serve synthetic UI demo data without changing network state")
	flag.Parse()

	if *dryRun {
		if err := printDryRun(*configPath, *arpPath, *ruleSetDir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Keep journals beside derived rule sets; the UI demo uses memory only.
	journalDir := filepath.Join(filepath.Dir(*ruleSetDir), "journal")
	if *uiDemo {
		journalDir = ""
	}
	journals := journal.NewSet(journalDir)
	defer func() {
		for _, j := range journals {
			j.Close()
		}
	}()
	log := slog.New(journal.NewHandler(slog.NewTextHandler(os.Stderr, nil), journals[journal.System]))

	cfg, err := model.Load(*configPath)
	if err != nil {
		log.Error("configuration", "err", err)
		os.Exit(1)
	}
	if *uiDemo {
		if err := serveAPI(ctx, cfg, api.NewDemo(cfg), log); err != nil {
			log.Error("web interface", "err", err)
			os.Exit(1)
		}
		return
	}

	fw := netfilter.NewFirewall()
	fw.IPTables, fw.Restore = *iptables, *restore
	d := daemon.New(daemon.Options{
		ConfigPath: *configPath, ARPPath: *arpPath, RuleSetDir: *ruleSetDir, WatchInterval: *watch,
		Firewall: fw, Log: log, Journals: journals, SingBoxLog: singBoxLog(journals[journal.System], log),
	})
	// A management API failure must not stop the independent routing daemon.
	go func() {
		if err := serveAPI(ctx, cfg, d, log); err != nil {
			log.Error("web interface failed to start", "err", err)
		}
	}()
	if err := d.Run(ctx); err != nil {
		log.Error("keenProxy stopped with an error", "err", err)
		os.Exit(1)
	}
}

// singBoxLog exposes a retained pipe to each engine instance and mirrors its
// output into the system journal and stderr. Linux uses /proc/self/fd.
func singBoxLog(j *journal.Journal, log *slog.Logger) string {
	r, w, err := os.Pipe()
	if err != nil {
		log.Warn("sing-box logs will not be collected", "err", err)
		return ""
	}
	go journal.ReadSingBox(r, j, os.Stderr)
	singBoxPipe = w
	return engine.PipeLogPath(w.Fd())
}

// singBoxPipe retains the writer so garbage collection cannot close the descriptor.
var singBoxPipe *os.File

// serveAPI serves management requests until cancellation, using startup bind
// and token settings; it does not apply routing changes itself.
func serveAPI(ctx context.Context, cfg *model.Config, d api.Daemon, log *slog.Logger) error {
	token, created, err := api.LoadToken(cfg.API.TokenFile)
	if err != nil {
		return fmt.Errorf("token: %w", err)
	}
	if created {
		log.Info("web interface login token created", "file", cfg.API.TokenFile, "token", token)
	}
	srv := &http.Server{
		Addr:              cfg.API.Listen,
		Handler:           (&api.Server{D: d, Token: token, UI: web.FS(), Log: log}).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()
	log.Info("web interface", "addr", "http://"+cfg.API.Listen)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// printDryRun renders derived settings without enabling interception; output may contain secrets.
func printDryRun(configPath, arpPath, ruleSetDir string) error {
	cfg, err := model.Load(configPath)
	if err != nil {
		return err
	}
	neighbors, _ := devices.ReadARP(arpPath, cfg.LAN.Interface)
	out, err := compile.Compile(compile.Input{Config: cfg, Neighbors: neighbors, RuleSetDir: ruleSetDir})
	if err != nil {
		return err
	}
	fmt.Println("# sing-box")
	fmt.Println(string(out.SingBox))
	names := make([]string, 0, len(out.RuleSets))
	for name := range out.RuleSets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Printf("\n# %s/%s\n%s\n", ruleSetDir, name, out.RuleSets[name])
	}
	fmt.Println("\n# iptables-restore --noflush")
	fmt.Print(out.Plan.Render())
	fmt.Println("\n# jumps")
	for _, j := range out.Plan.Jumps() {
		fmt.Printf("iptables -t %s -I %s 1 -j %s\n", j.Table, j.From, j.To)
	}
	return nil
}
