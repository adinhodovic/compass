package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/adinhodovic/compass/internal/catalog"
	"github.com/adinhodovic/compass/internal/config"
	"github.com/adinhodovic/compass/internal/registry"
	"github.com/adinhodovic/compass/internal/server"
	"github.com/adinhodovic/compass/internal/source"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	listenAddress string
	configFile    string
)

var rootCmd = &cobra.Command{
	Use:   "compass",
	Short: "Service-discovery dashboard for homelabs",
	Long:  `Compass discovers services from your homelab (Docker, Kubernetes, Tailscale, Headscale, JSON APIs) and renders them with operational context.`,
	RunE:  runCompass,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// flagSpec describes one CLI flag plus the env var it binds to. `short` is
// the short flag rune (or 0 for none); `value` is the variable the flag
// writes to (so `viper.GetString` and the variable both stay in sync).
type flagSpec struct {
	name   string
	short  string
	env    string
	value  *string
	defVal string
	usage  string
}

// flagSpecs lists the only two CLI flags the binary accepts. Source-specific
// settings (tailnet IDs, API keys, docker hosts, …) come from compass.yaml,
// where ${VAR} interpolation handles secrets — that scales to multiple
// tailnets / clusters in one config without dedicated flags per source.
var flagSpecs = []flagSpec{
	{
		name:   "listen-address",
		short:  "l",
		env:    "COMPASS_LISTEN_ADDRESS",
		value:  &listenAddress,
		defVal: ":8080",
		usage:  "Address to listen on",
	},
	{
		name:   "config",
		short:  "c",
		env:    "COMPASS_CONFIG",
		value:  &configFile,
		defVal: "compass.yaml",
		usage:  "Path to Compass config",
	},
}

func init() {
	flags := rootCmd.PersistentFlags()
	for _, f := range flagSpecs {
		if f.short != "" {
			flags.StringVarP(f.value, f.name, f.short, f.defVal, f.usage)
		} else {
			flags.StringVar(f.value, f.name, f.defVal, f.usage)
		}
	}

	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()
	for _, f := range flagSpecs {
		if err := viper.BindPFlag(f.name, flags.Lookup(f.name)); err != nil {
			panic(fmt.Errorf("bind flag %s: %w", f.name, err))
		}
		if err := viper.BindEnv(f.name, f.env); err != nil {
			panic(fmt.Errorf("bind env %s: %w", f.name, err))
		}
	}
}

func runCompass(cmd *cobra.Command, args []string) error {
	listenAddress = strings.TrimSpace(viper.GetString("listen-address"))
	configFile = strings.TrimSpace(viper.GetString("config"))
	if configFile == "" {
		return errors.New("config path is required")
	}

	cfg, err := config.Load(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	logger := buildLogger(cfg.Logging)
	logger.Info("Starting Compass", "version", version, "commit", commit, "build_time", buildTime)

	// Don't share http.DefaultClient — other libraries can mutate it. Cap
	// individual requests at 30s as belt-and-suspenders alongside the
	// registry's per-refresh ctx timeout.
	httpClient := &http.Client{Timeout: 30 * time.Second}
	entries, err := source.BuildSources(cfg, httpClient)
	if err != nil {
		return fmt.Errorf("failed to build sources: %w", err)
	}
	catalogDB, err := catalog.Load(cfg.Catalog.Path)
	if err != nil {
		return fmt.Errorf("failed to load catalog: %w", err)
	}

	reg := registry.NewFromEntries(entries, logger, catalogDB, registryOptionsFromConfig(cfg)...)
	defer func() {
		if err := reg.Close(); err != nil {
			logger.Warn("Source close reported errors", "err", err)
		}
	}()
	services, err := reg.Load(cmd.Context())
	if err != nil {
		// Partial load is logged but not fatal; some sources may have failed
		// and registry already returned what did succeed.
		logger.Warn("Source load reported errors", "err", err)
	}
	logger.Info("Loaded services", "count", len(services))

	watchCtx, cancelWatch := context.WithCancel(cmd.Context())
	defer cancelWatch()
	reg.Watch(watchCtx)

	handler := server.New(cfg, reg, logger)
	httpServer := &http.Server{
		Addr:              listenAddress,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		sighup := make(chan os.Signal, 1)
		signal.Notify(sighup, syscall.SIGHUP)
		for range sighup {
			logger.Info("SIGHUP received, refreshing all sources")
			if err := reg.Refresh(cmd.Context()); err != nil {
				logger.Warn("Refresh reported errors", "err", err)
			}
		}
	}()

	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)
		<-sigint

		logger.Info("Received interrupt signal, shutting down")
		cancelWatch()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			logger.Error("HTTP server shutdown error", "err", err)
		}
	}()

	logger.Info("Listening", "address", listenAddress)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("HTTP server failed: %w", err)
	}

	logger.Info("Compass stopped")
	return nil
}

func SetVersionInfo(v, c, bt string) {
	version = v
	commit = c
	buildTime = bt
}

// buildLogger constructs the slog logger from the validated config.
// config.Load already lowercased and validated format/level, so this is
// straight-line dispatch.
func buildLogger(cfg config.LoggingConfig) *slog.Logger {
	var level slog.Level
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	if cfg.Format == "json" {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}

func registryOptionsFromConfig(cfg config.Config) []registry.Option {
	filters := cfg.Services.Filters
	dedupeWWW := true
	if filters.DedupeWWW != nil {
		dedupeWWW = *filters.DedupeWWW
	}
	excludeWildcards := true
	if filters.ExcludeWildcardHosts != nil {
		excludeWildcards = *filters.ExcludeWildcardHosts
	}
	return []registry.Option{registry.WithFilters(registry.Filters{
		ExcludeURLPatterns:   filters.ExcludeURLPatterns,
		DedupeWWW:            dedupeWWW,
		ExcludeWildcardHosts: excludeWildcards,
	})}
}
