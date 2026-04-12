package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/RCooLeR/jinko-exporter/internal/alert"
	"github.com/RCooLeR/jinko-exporter/internal/config"
	"github.com/RCooLeR/jinko-exporter/internal/poller"
	"github.com/RCooLeR/jinko-exporter/internal/prom"
	"github.com/RCooLeR/jinko-exporter/internal/source"
	"github.com/RCooLeR/jinko-exporter/internal/source/jinko"
	"github.com/RCooLeR/jinko-exporter/internal/source/modbus"
	"github.com/RCooLeR/jinko-exporter/internal/source/solarman"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
)

func Run(args []string) int {
	app := &cli.App{
		Name:  "jinko-exporter",
		Usage: "Poll solar data from Jinko detail API, Solarman OpenAPI, or a future Modbus source and expose Prometheus metrics",
		Flags: config.Flags(),
		Before: func(ctx *cli.Context) error {
			cfg, err := config.FromCLI(ctx)
			if err != nil {
				return err
			}
			setupLogger(cfg.LogLevel)
			return nil
		},
		Commands: []*cli.Command{
			{
				Name:  "serve",
				Usage: "Run the exporter HTTP server",
				Action: func(ctx *cli.Context) error {
					cfg, err := config.FromCLI(ctx)
					if err != nil {
						return err
					}
					return runServe(ctx.Context, cfg)
				},
			},
			{
				Name:  "fetch",
				Usage: "Fetch once and print the normalized snapshot as JSON",
				Action: func(ctx *cli.Context) error {
					cfg, err := config.FromCLI(ctx)
					if err != nil {
						return err
					}
					return runFetch(ctx.Context, cfg)
				},
			},
		},
		Action: func(ctx *cli.Context) error {
			cfg, err := config.FromCLI(ctx)
			if err != nil {
				return err
			}
			return runServe(ctx.Context, cfg)
		},
	}

	if err := app.Run(args); err != nil {
		log.Error().Err(err).Msg("application failed")
		return 1
	}
	return 0
}

func setupLogger(level string) {
	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		lvl = zerolog.InfoLevel
	}

	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).
		Level(lvl).
		With().
		Timestamp().
		Logger()
}

func runServe(parent context.Context, cfg config.Config) error {
	alerts, err := buildAlerts(cfg)
	if err != nil {
		return err
	}

	src, err := buildSource(cfg, alerts)
	if err != nil {
		return err
	}

	state := poller.NewState(src.Name())
	runner := poller.NewRunner(src, cfg.PollInterval, state, alerts, cfg.Alerts)

	ctx, cancel := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	registry := prometheus.NewRegistry()
	collector := prom.NewCollector(cfg.MetricPrefix, state, cfg.DropSourceLabel)
	if err := registry.Register(collector); err != nil {
		return fmt.Errorf("register collector: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle(cfg.MetricsPath, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if state.HasSnapshot() {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ready\n"))
			return
		}
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	})

	server := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go runner.Run(ctx)

	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error().Err(err).Msg("http shutdown failed")
		}
	}()

	log.Info().
		Str("source_priority", src.Name()).
		Str("listen", cfg.ListenAddress).
		Str("metrics_path", cfg.MetricsPath).
		Dur("poll_interval", cfg.PollInterval).
		Msg("starting exporter")

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func runFetch(ctx context.Context, cfg config.Config) error {
	alerts, err := buildAlerts(cfg)
	if err != nil {
		return err
	}

	src, err := buildSource(cfg, alerts)
	if err != nil {
		return err
	}

	snapshot, err := src.Fetch(ctx)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(snapshot)
}

func buildSource(cfg config.Config, alerts *alert.Manager) (source.Source, error) {
	sources := make([]source.Source, 0, len(cfg.SourcePriority))
	for _, sourceName := range cfg.SourcePriority {
		src, err := buildSingleSource(sourceName, cfg, alerts)
		if err != nil {
			return nil, err
		}
		sources = append(sources, src)
	}
	if len(sources) == 1 {
		return sources[0], nil
	}
	return source.NewPriority(sources, cfg.DropSourceLabel), nil
}

func buildSingleSource(sourceName string, cfg config.Config, alerts *alert.Manager) (source.Source, error) {
	switch sourceName {
	case "jinko":
		return jinko.New(cfg.Jinko, alerts), nil
	case "solarman":
		return solarman.New(cfg.Solarman, alerts), nil
	case "modbus":
		return modbus.New(cfg.Modbus), nil
	default:
		return nil, fmt.Errorf("unsupported source %q", sourceName)
	}
}

func buildAlerts(cfg config.Config) (*alert.Manager, error) {
	if !cfg.Alerts.Enabled {
		return nil, nil
	}

	notifier, err := alert.NewSMTPNotifier(cfg.Alerts)
	if err != nil {
		return nil, fmt.Errorf("build SMTP notifier: %w", err)
	}
	return alert.NewManager(notifier, cfg.Alerts.Cooldown), nil
}
