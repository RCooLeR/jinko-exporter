package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/alert"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/hamqtt"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/poller"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/prom"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source/jinko"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source/modbus"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source/shelly"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source/solarman"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"
)

const (
	httpReadHeaderTimeout = 5 * time.Second
	httpReadTimeout       = 10 * time.Second
	httpWriteTimeout      = 30 * time.Second
	httpIdleTimeout       = 60 * time.Second
	httpMaxHeaderBytes    = 1 << 20
	httpMaxHeaderValues   = 100
)

func Run(args []string) int {
	command := &cli.Command{
		Name:  "jinko-exporter",
		Usage: "Poll solar data from Jinko detail API, Solarman OpenAPI, or the locked read-only Modbus profile and expose Prometheus metrics",
		Flags: config.Flags(),
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			setupLogger(cmd.String("log-level"))
			return ctx, nil
		},
		Commands: []*cli.Command{
			{
				Name:  "serve",
				Usage: "Run the exporter HTTP server",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					cfg, err := config.FromCLI(cmd)
					if err != nil {
						return err
					}
					return runServe(ctx, cfg)
				},
			},
			{
				Name:  "fetch",
				Usage: "Fetch once and print the normalized snapshot as JSON",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					cfg, err := config.FromCLI(cmd)
					if err != nil {
						return err
					}
					return runFetch(ctx, cfg)
				},
			},
			{
				Name:  "healthcheck",
				Usage: "Check the exporter HTTP health endpoint",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "url", Usage: "Health endpoint URL; defaults to EXPORTER_LISTEN with /healthz", Sources: cli.EnvVars("EXPORTER_HEALTHCHECK_URL")},
					&cli.DurationFlag{Name: "timeout", Value: 5 * time.Second, Usage: "Healthcheck timeout", Sources: cli.EnvVars("EXPORTER_HEALTHCHECK_TIMEOUT")},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					endpoint := cmd.String("url")
					if endpoint == "" {
						var err error
						endpoint, err = defaultHealthcheckURL(cmd.String("listen"))
						if err != nil {
							return err
						}
					}
					return runHealthcheck(ctx, endpoint, cmd.Duration("timeout"))
				},
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfg, err := config.FromCLI(cmd)
			if err != nil {
				return err
			}
			return runServe(ctx, cfg)
		},
	}

	if err := command.Run(context.Background(), args); err != nil {
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

	ctx, cancel := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	var maintenanceDone <-chan struct{}
	var runnerDone <-chan struct{}
	var mqttPublisher *hamqtt.Publisher
	defer func() {
		var closePublisher func()
		if mqttPublisher != nil {
			closePublisher = mqttPublisher.Close
		}
		stopServeWorkers(cancel, maintenanceDone, runnerDone, closePublisher)
	}()

	var observers []poller.Observer
	if cfg.MQTT.Enabled {
		mqttPublisher, err = hamqtt.NewPublisher(cfg.MQTT)
		if err != nil {
			return err
		}
		observers = append(observers, mqttPublisher)
	}

	runner := poller.NewRunner(src, cfg.PollInterval, state, alerts, cfg.Alerts, observers...)

	registry := prometheus.NewRegistry()
	collector := prom.NewCollector(cfg.MetricPrefix, state, cfg.DropSourceLabel)
	if err := registry.Register(collector); err != nil {
		return fmt.Errorf("register collector: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle(cfg.MetricsPath, promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		CoalesceGather: true,
	}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if state.Ready(3 * cfg.PollInterval) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ready\n"))
			return
		}
		http.Error(w, "not ready or stale", http.StatusServiceUnavailable)
	})

	server := newHTTPServer(cfg.ListenAddress, mux)
	listener, err := net.Listen("tcp", cfg.ListenAddress)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.ListenAddress, err)
	}
	defer func() { _ = listener.Close() }()
	if mqttPublisher != nil {
		if err := mqttPublisher.Start(); err != nil {
			return err
		}
	}

	maintenanceDone = startBackgroundMaintenance(ctx, src)

	runnerStopped := make(chan struct{})
	runnerDone = runnerStopped
	go func() {
		defer close(runnerStopped)
		runner.Run(ctx)
	}()

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
		Bool("mqtt_enabled", cfg.MQTT.Enabled).
		Msg("starting exporter")

	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func newHTTPServer(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:                address,
		Handler:             handler,
		ReadHeaderTimeout:   httpReadHeaderTimeout,
		ReadTimeout:         httpReadTimeout,
		WriteTimeout:        httpWriteTimeout,
		IdleTimeout:         httpIdleTimeout,
		MaxHeaderBytes:      httpMaxHeaderBytes,
		MaxHeaderValueCount: httpMaxHeaderValues,
	}
}

func stopServeWorkers(cancel context.CancelFunc, maintenanceDone, runnerDone <-chan struct{}, closePublisher func()) {
	cancel()
	if maintenanceDone != nil {
		<-maintenanceDone
	}
	if runnerDone != nil {
		<-runnerDone
	}
	if closePublisher != nil {
		closePublisher()
	}
}

func startBackgroundMaintenance(ctx context.Context, src source.Source) <-chan struct{} {
	done := make(chan struct{})
	maintainer, ok := src.(source.BackgroundMaintainer)
	if !ok {
		close(done)
		return done
	}
	go func() {
		defer close(done)
		maintainer.RunBackground(ctx)
	}()
	return done
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

func defaultHealthcheckURL(listenAddress string) (string, error) {
	host, port, err := net.SplitHostPort(listenAddress)
	if err != nil {
		return "", fmt.Errorf("build healthcheck URL from listen address: %w", err)
	}
	if port == "" {
		return "", fmt.Errorf("build healthcheck URL from listen address: port is required")
	}

	switch strings.TrimSpace(host) {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}

	u := url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(host, port),
		Path:   "/healthz",
	}
	return u.String(), nil
}

func runHealthcheck(parent context.Context, endpoint string, timeout time.Duration) error {
	if timeout <= 0 {
		return fmt.Errorf("healthcheck timeout must be > 0")
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("healthcheck failed: status=%d", resp.StatusCode)
	}
	return nil
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
		return enrichSource(sources[0], cfg)
	}
	return enrichSource(source.NewPriority(sources, cfg.ProjectFailoverMetrics), cfg)
}

func buildSingleSource(sourceName string, cfg config.Config, alerts *alert.Manager) (source.Source, error) {
	switch sourceName {
	case "jinko":
		return jinko.New(cfg.Jinko, alerts), nil
	case "solarman":
		return solarman.New(cfg.Solarman, alerts), nil
	case "modbus":
		return modbus.New(cfg.Modbus)
	default:
		return nil, fmt.Errorf("unsupported source %q", sourceName)
	}
}

func enrichSource(src source.Source, cfg config.Config) (source.Source, error) {
	if !cfg.ShellyGridLoad.Enabled {
		return src, nil
	}
	gridLoad, err := shelly.NewGridLoadClient(cfg.ShellyGridLoad)
	if err != nil {
		return nil, err
	}
	return source.NewEnriched(src, gridLoad), nil
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
