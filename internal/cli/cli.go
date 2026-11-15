package cli

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/galq/blackflame/internal/auth"
	"github.com/galq/blackflame/internal/config"
	"github.com/galq/blackflame/internal/discovery"
	"github.com/galq/blackflame/internal/events"
	"github.com/galq/blackflame/internal/flameimport"
	"github.com/galq/blackflame/internal/icons"
	"github.com/galq/blackflame/internal/metrics"
	"github.com/galq/blackflame/internal/runtime"
	"github.com/galq/blackflame/internal/server"
	"github.com/galq/blackflame/internal/status"
	"github.com/galq/blackflame/internal/store"
	"github.com/galq/blackflame/internal/version"
	"github.com/galq/blackflame/internal/weather"
	"github.com/galq/blackflame/internal/widgets"
)

func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}
	switch args[0] {
	case "serve":
		return runServe(args[1:], stdout, stderr)
	case "validate":
		return runValidate(args[1:], stdout, stderr)
	case "version":
		fmt.Fprintf(stdout, "blackflame %s (commit %s, built %s, %s)\n", version.Version, version.Commit, version.BuildDate, version.Get().GoVersion)
		return 0
	case "healthcheck":
		return runHealthcheck(args[1:], stdout, stderr)
	case "import":
		return runImport(args[1:], stdout, stderr)
	case "export":
		return runExport(args[1:], stdout, stderr)
	case "migrate":
		return runMigrate(args[1:], stdout, stderr)
	case "backup":
		return runBackup(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `Blackflame — a self-hosted homelab dashboard.

Usage:
  blackflame serve --mode ui --data ./data [--listen :8080]
  blackflame serve --mode gitops --config ./blackflame.yaml [--data ./data]
  blackflame validate --config ./blackflame.yaml
  blackflame import flame --db ./db.sqlite --data ./data [--replace]
  blackflame import flame --db ./db.sqlite --output ./blackflame.yaml
  blackflame export --data ./data [--output ./blackflame.yaml]
  blackflame migrate --data ./data
  blackflame backup --data ./data --output ./blackflame-backup.db
  blackflame healthcheck [--url http://127.0.0.1:8080/healthz]
  blackflame version

Environment:
  BLACKFLAME_PASSWORD       Initial edit-mode password (optional)
  BLACKFLAME_PASSWORD_FILE  Read the password from a file (optional)
`)
}

func runServe(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	modeValue := flags.String("mode", "ui", "configuration mode: ui or gitops")
	dataDir := flags.String("data", "./data", "data directory (or empty for in-memory GitOps runtime)")
	configPath := flags.String("config", "", "YAML config path (required in gitops mode)")
	listen := flags.String("listen", ":8080", "HTTP listen address")
	basePath := flags.String("base-path", "/", "base path when served behind a reverse proxy")
	logLevel := flags.String("log-level", "info", "log level: debug, info, warn, error")
	logFormat := flags.String("log-format", "json", "log format: json or text")
	shutdownTimeout := flags.Duration("shutdown-timeout", 10*time.Second, "graceful shutdown timeout")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	// Keep the Go heap bounded by default. GOMEMLIMIT is a soft limit; users
	// can override it explicitly for unusually large deployments.
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(64 << 20)
	}

	mode, err := config.ParseMode(*modeValue)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	logger := newLogger(*logLevel, *logFormat, stdout)
	if mode == config.ModeGitOps && strings.TrimSpace(*configPath) == "" {
		fmt.Fprintln(stderr, "--config is required in gitops mode")
		return 2
	}
	if mode == config.ModeUI && strings.TrimSpace(*configPath) != "" {
		fmt.Fprintln(stderr, "--config is only valid in gitops mode")
		return 2
	}

	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(signalCtx)
	defer cancel()

	cfgStore, db, err := buildConfigStore(ctx, mode, *dataDir, *configPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer db.Close()
	defer cfgStore.Close()

	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-hup:
				if err := cfgStore.Reload(ctx); err != nil {
					logger.Warn("config reload failed", "error", err)
				}
			}
		}
	}()

	envPassword, err := readPassword()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	authManager, err := auth.New(ctx, db, envPassword)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	cfg := cfgStore.Snapshot().Config
	if cfg == nil {
		fmt.Fprintln(stderr, "configuration is not loaded")
		return 1
	}

	hub := events.NewHub()
	registry := metrics.New()
	weatherClient := weather.New()

	app := &server.App{
		Mode:    mode,
		Store:   cfgStore,
		DB:      db,
		Runtime: runtime.NewSQLite(db),
		Auth:    authManager,
		Icons:   icons.New(db, cfg.Icons.Source, logger),
		Weather: weatherClient,
		Widgets: widgets.New(logger),
		Events:  hub,
		Metrics: registry,
		Logger:  logger,
		Version: version.Get(),
		Started: time.Now(),
	}

	statusManager := status.New(db, hub, logger, registry, func() *config.Config {
		return app.Config(context.Background())
	})
	app.Status = statusManager

	discoveryManager := discovery.New(
		discoverySink{db: db},
		logger,
		registry,
		func() *config.Config { return app.Config(context.Background()) },
		func(ctx context.Context) { _ = app.Reload(ctx) },
	)
	app.Discovery = discoveryManager

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		statusManager.Start(ctx)
	}()
	go func() {
		defer wg.Done()
		discoveryManager.Start(ctx)
	}()

	httpServer := server.New(app, *listen, *basePath, *shutdownTimeout)
	logger.Info("starting blackflame", "mode", mode, "version", version.Version, "data", *dataDir, "config", *configPath)
	err = httpServer.Run(ctx)
	cancel()
	wg.Wait()
	if err != nil {
		logger.Error("server stopped with error", "error", err)
		return 1
	}
	return 0
}

func buildConfigStore(ctx context.Context, mode config.Mode, dataDir, configPath string) (config.Store, *sql.DB, error) {
	db, err := openDatabase(mode, dataDir)
	if err != nil {
		return nil, nil, err
	}
	if err := store.Migrate(ctx, db); err != nil {
		db.Close()
		return nil, nil, err
	}
	if err := store.EnsureDefaults(ctx, db); err != nil {
		db.Close()
		return nil, nil, err
	}

	switch mode {
	case config.ModeUI:
		cfgStore, err := store.NewSQLiteStore(ctx, db)
		if err != nil {
			db.Close()
			return nil, nil, err
		}
		return cfgStore, db, nil
	case config.ModeGitOps:
		cfgStore, err := config.NewYAMLStore(ctx, configPath)
		if err != nil {
			db.Close()
			return nil, nil, err
		}
		return cfgStore, db, nil
	default:
		db.Close()
		return nil, nil, fmt.Errorf("unsupported mode %q", mode)
	}
}

func openDatabase(mode config.Mode, dataDir string) (*sql.DB, error) {
	if mode == config.ModeGitOps && strings.TrimSpace(dataDir) == "" {
		return store.OpenSQLite(":memory:")
	}
	if strings.TrimSpace(dataDir) == "" {
		return nil, fmt.Errorf("--data is required in ui mode")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	return store.OpenSQLite(filepath.Join(dataDir, "blackflame.db"))
}

func readPassword() (string, error) {
	if path := strings.TrimSpace(os.Getenv("BLACKFLAME_PASSWORD_FILE")); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read BLACKFLAME_PASSWORD_FILE: %w", err)
		}
		return strings.TrimSpace(string(raw)), nil
	}
	return strings.TrimSpace(os.Getenv("BLACKFLAME_PASSWORD")), nil
}

func runValidate(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("config", "", "YAML config path")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *path == "" {
		fmt.Fprintln(stderr, "--config is required")
		return 2
	}
	cfg, err := config.LoadFile(*path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	boards, sections, items := cfg.Counts()
	fmt.Fprintf(stdout, "config valid: %d boards, %d sections, %d items\n", boards, sections, items)
	return 0
}

func runHealthcheck(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	flags.SetOutput(stderr)
	url := flags.String("url", "http://127.0.0.1:8080/healthz", "health endpoint")
	timeout := flags.Duration("timeout", 5*time.Second, "request timeout")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	client := &http.Client{Timeout: *timeout}
	resp, err := client.Get(*url)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		fmt.Fprintf(stderr, "healthcheck returned %d\n", resp.StatusCode)
		return 1
	}
	fmt.Fprintln(stdout, "ok")
	return 0
}

func runImport(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "flame" {
		fmt.Fprintln(stderr, "usage: blackflame import flame --db ./db.sqlite --data ./data [--replace]")
		return 2
	}
	flags := flag.NewFlagSet("import flame", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dbPath := flags.String("db", "", "Flame db.sqlite path")
	dataDir := flags.String("data", "./data", "Blackflame data directory")
	output := flags.String("output", "", "write a GitOps YAML file instead of importing into a data directory")
	replace := flags.Bool("replace", false, "replace existing boards and content")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if *dbPath == "" {
		fmt.Fprintln(stderr, "--db is required")
		return 2
	}
	ctx := context.Background()
	var db *sql.DB
	var err error
	if *output != "" {
		db, err = store.OpenSQLite(":memory:")
	} else {
		db, err = openDatabase(config.ModeUI, *dataDir)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer db.Close()
	if err := store.Migrate(ctx, db); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := store.EnsureDefaults(ctx, db); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	result, err := flameimport.Import(ctx, db, *dbPath, *replace || *output != "")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *output != "" {
		cfg, err := store.LoadConfig(ctx, db)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		data, err := yaml.Marshal(cfg)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := os.WriteFile(*output, data, 0o644); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "imported %d apps, %d categories, %d bookmarks (%d skipped) to %s\n", result.Apps, result.Categories, result.Bookmarks, result.Skipped, *output)
		return 0
	}
	fmt.Fprintf(stdout, "imported %d apps, %d categories, %d bookmarks (%d skipped)\n", result.Apps, result.Categories, result.Bookmarks, result.Skipped)
	return 0
}

func runMigrate(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dataDir := flags.String("data", "./data", "Blackflame data directory")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	ctx := context.Background()
	db, err := openDatabase(config.ModeUI, *dataDir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer db.Close()
	if err := store.Migrate(ctx, db); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := store.EnsureDefaults(ctx, db); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, "migrations complete")
	return 0
}

func runBackup(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("backup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dataDir := flags.String("data", "./data", "Blackflame data directory")
	output := flags.String("output", "blackflame-backup.db", "backup output path")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	ctx := context.Background()
	db, err := openDatabase(config.ModeUI, *dataDir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer db.Close()
	if err := store.Migrate(ctx, db); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, *output); err != nil {
		fmt.Fprintln(stderr, fmt.Errorf("backup: %w", err))
		return 1
	}
	fmt.Fprintf(stdout, "backup written to %s\n", *output)
	return 0
}

func runExport(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("export", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dataDir := flags.String("data", "./data", "Blackflame data directory")
	output := flags.String("output", "", "output file (default stdout)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	ctx := context.Background()
	db, err := openDatabase(config.ModeUI, *dataDir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer db.Close()
	if err := store.Migrate(ctx, db); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	cfg, err := store.LoadConfig(ctx, db)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *output == "" {
		_, _ = stdout.Write(data)
		return 0
	}
	if err := os.WriteFile(*output, data, 0o644); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "wrote %s\n", *output)
	return 0
}

func newLogger(levelValue, format string, out io.Writer) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(levelValue) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	options := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if strings.EqualFold(format, "text") {
		handler = slog.NewTextHandler(out, options)
	} else {
		handler = slog.NewJSONHandler(out, options)
	}
	return slog.New(handler)
}

type discoverySink struct {
	db *sql.DB
}

func (s discoverySink) UpsertDiscovered(ctx context.Context, source string, items []config.DiscoveredItem) error {
	return store.UpsertDiscovered(ctx, s.db, source, items)
}
