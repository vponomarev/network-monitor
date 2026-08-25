//go:build linux
// +build linux

package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"github.com/vponomarev/network-monitor/internal/buildinfo"
	"github.com/vponomarev/network-monitor/internal/config"
	"github.com/vponomarev/network-monitor/internal/conntrack"
	"github.com/vponomarev/network-monitor/pkg/embedded"
	"go.uber.org/zap"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"

	ebpfProgram    string
	configFile     string
	syslogNetwork  string
	syslogAddress  string
	syslogTag      string
	syslogFacility string
	syslogHostname bool
	synTimeout     string
	trackIncoming  bool
	trackOutgoing  bool
	trackCloses    bool
	showConfig     bool
	installPath    string
	exportEBPFPath string
)

func main() {
	buildInfo := buildinfo.New("conntrack", Version, GitCommit, BuildTime)
	rootCmd := &cobra.Command{
		Use:     "conntrack",
		Short:   "Connection Tracker",
		Long:    "eBPF-based connection tracking for incoming and outgoing network connections (Linux only)",
		Version: Version,
		RunE:    run,
	}
	rootCmd.SetVersionTemplate(fmt.Sprintf(
		"conntrack %s (commit=%s, built=%s, go=%s, %s/%s)\n",
		buildInfo.Version, buildInfo.GitCommit, buildInfo.BuildTime,
		buildInfo.GoVersion, buildInfo.GOOS, buildInfo.GOARCH,
	))

	// Основные флаги
	rootCmd.Flags().StringVarP(&ebpfProgram, "ebpf-prog", "p", "", "Path to eBPF program object file")
	rootCmd.Flags().StringVarP(&configFile, "config", "c", "", "Config file path")
	rootCmd.Flags().BoolVar(&showConfig, "show-config", false, "Print sample configuration and exit")
	rootCmd.Flags().StringVar(&installPath, "install-path", "/usr/local/bin", "Installation path")
	rootCmd.Flags().StringVar(&exportEBPFPath, "export-ebpf-prog", "", "Export embedded eBPF program to file")

	// Команды
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(deinstallCmd)
	rootCmd.AddCommand(rollbackCmd)
	rootCmd.AddCommand(showConfigCmd)

	// Syslog flags
	rootCmd.Flags().StringVar(&syslogNetwork, "syslog-network", "", "Syslog network type (empty for local, 'udp' or 'tcp' for remote)")
	rootCmd.Flags().StringVar(&syslogAddress, "syslog-addr", "", "Syslog address (e.g., 'localhost:514' for remote)")
	rootCmd.Flags().StringVar(&syslogTag, "syslog-tag", "conntrack", "Syslog tag/program name")
	rootCmd.Flags().StringVar(&syslogFacility, "syslog-facility", "LOCAL0", "Syslog facility (LOCAL0-7, USER, DAEMON)")
	rootCmd.Flags().BoolVar(&syslogHostname, "syslog-hostname", true, "Include hostname in syslog messages")

	// Tracking options
	rootCmd.Flags().StringVar(&synTimeout, "syn-timeout", "30s", "Timeout for waiting SYN+ACK")
	rootCmd.Flags().BoolVar(&trackIncoming, "track-incoming", true, "Track incoming connections")
	rootCmd.Flags().BoolVar(&trackOutgoing, "track-outgoing", true, "Track outgoing connections")
	rootCmd.Flags().BoolVar(&trackCloses, "track-closes", true, "Track connection closes")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	// Check platform
	if err := checkPlatform(); err != nil {
		return err
	}

	// Обработка --show-config (флаг)
	if showConfig {
		// Команда show-config обрабатывается отдельно, но флаг тоже поддерживается
		data, err := embedded.GetSampleConfig()
		if err != nil {
			return fmt.Errorf("failed to get sample config: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	// Обработка --export-ebpf-prog
	if exportEBPFPath != "" {
		if err := embedded.ExportEBPFToFile(exportEBPFPath); err != nil {
			return fmt.Errorf("exporting eBPF program: %w", err)
		}
		fmt.Printf("✓ Exported embedded eBPF program to: %s\n", exportEBPFPath)
		return nil
	}

	// Load configuration
	cfg, err := config.Load(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize logger
	logger, err := initLogger(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	defer func() { _ = logger.Sync() }()

	logger.Info("Starting Connection Tracker",
		zap.String("version", Version),
		zap.String("ebpf_program", ebpfProgram),
	)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		logger.Info("Received signal, shutting down", zap.String("signal", sig.String()))
		cancel()
	}()

	// Parse syslog facility
	facility, err := parseSyslogFacility(syslogFacility)
	if err != nil {
		return fmt.Errorf("invalid syslog facility: %w", err)
	}

	// Parse SYN timeout
	timeout, err := time.ParseDuration(synTimeout)
	if err != nil {
		return fmt.Errorf("invalid syn-timeout: %w", err)
	}

	trackerCfg := conntrack.Config{
		// An empty path tells Tracker to use the embedded production object.
		// A non-empty path is an explicit operator override.
		EBPFProgramPath:       ebpfProgram,
		TrackIncoming:         trackIncoming && cfg.Connections.TrackIncoming,
		TrackOutgoing:         trackOutgoing && cfg.Connections.TrackOutgoing,
		TrackCloses:           trackCloses,
		FilterPorts:           cfg.Connections.FilterPorts,
		Registerer:            prometheus.DefaultRegisterer,
		EventBufferSize:       cfg.Connections.EventBufferSize,
		StateTTL:              cfg.Connections.StateTTLDuration(),
		CleanupInterval:       cfg.Connections.CleanupIntervalDuration(),
		MaxTrackedConnections: cfg.Connections.MaxTrackedConnections,
		MaxPendingConnections: cfg.Connections.MaxPendingConnections,
		Syslog: conntrack.SyslogConfig{
			Network:         syslogNetwork,
			Address:         syslogAddress,
			Tag:             syslogTag,
			Facility:        facility,
			IncludeHostname: syslogHostname,
		},
		SYNTimeout: timeout,
	}

	// Initialize connection tracker
	tracker, err := conntrack.NewTracker(trackerCfg, logger)
	if err != nil {
		return fmt.Errorf("failed to create connection tracker: %w", err)
	}

	return runServices(ctx, tracker, cfg, logger)
}

func runServices(ctx context.Context, tracker *conntrack.Tracker, cfg *config.Config, logger *zap.Logger) error {
	serviceCtx, stopServices := context.WithCancel(ctx)
	defer stopServices()
	mux := http.NewServeMux()
	requireAuth := func(handler http.Handler) http.Handler {
		if cfg.Global.AuthToken == "" {
			return handler
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+cfg.Global.AuthToken)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			handler.ServeHTTP(w, r)
		})
	}
	buildInfo := buildinfo.New("conntrack", Version, GitCommit, BuildTime)
	prometheus.MustRegister(buildInfo.Collector())
	mux.Handle("/metrics", requireAuth(promhttp.Handler()))
	mux.Handle("/api/v1/version", requireAuth(buildInfo.Handler()))
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
		if !tracker.Ready() {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready\n"))
	})

	addr := net.JoinHostPort(cfg.Global.MetricsAddr, fmt.Sprintf("%d", cfg.Global.MetricsPort))
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("Starting conntrack HTTP server", zap.String("address", addr))
		serverErr <- server.ListenAndServe()
	}()
	trackerErr := make(chan error, 1)
	go func() { trackerErr <- tracker.Run(serviceCtx) }()

	var result error
	trackerFinished := false
	select {
	case <-ctx.Done():
	case err := <-trackerErr:
		trackerFinished = true
		if err != nil {
			result = fmt.Errorf("connection tracker error: %w", err)
		}
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			result = fmt.Errorf("conntrack HTTP server: %w", err)
		}
	}
	stopServices()
	if !trackerFinished {
		select {
		case err := <-trackerErr:
			if err != nil && result == nil {
				result = fmt.Errorf("connection tracker error: %w", err)
			}
		case <-time.After(5 * time.Second):
			if result == nil {
				result = fmt.Errorf("timed out stopping connection tracker")
			}
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil && result == nil {
		result = fmt.Errorf("shutting down conntrack HTTP server: %w", err)
	}
	return result
}

func initLogger(cfg *config.Config) (*zap.Logger, error) {
	var zapCfg zap.Config

	switch cfg.Logging.Format {
	case "json":
		zapCfg = zap.NewProductionConfig()
	default:
		zapCfg = zap.NewDevelopmentConfig()
	}

	level := zap.InfoLevel
	switch cfg.Logging.Level {
	case "debug":
		level = zap.DebugLevel
	case "warn":
		level = zap.WarnLevel
	case "error":
		level = zap.ErrorLevel
	}

	zapCfg.Level = zap.NewAtomicLevelAt(level)

	// Default to stdout/stderr
	zapCfg.OutputPaths = []string{"stdout"}
	zapCfg.ErrorOutputPaths = []string{"stderr"}

	return zapCfg.Build()
}

func checkPlatform() error {
	// This function only exists on Linux builds
	return nil
}

func parseSyslogFacility(s string) (conntrack.SyslogFacility, error) {
	switch s {
	case "USER", "user":
		return conntrack.LogUser, nil
	case "DAEMON", "daemon":
		return conntrack.LogDaemon, nil
	case "LOCAL0", "local0":
		return conntrack.LogLocal0, nil
	case "LOCAL1", "local1":
		return conntrack.LogLocal1, nil
	case "LOCAL2", "local2":
		return conntrack.LogLocal2, nil
	case "LOCAL3", "local3":
		return conntrack.LogLocal3, nil
	case "LOCAL4", "local4":
		return conntrack.LogLocal4, nil
	case "LOCAL5", "local5":
		return conntrack.LogLocal5, nil
	case "LOCAL6", "local6":
		return conntrack.LogLocal6, nil
	case "LOCAL7", "local7":
		return conntrack.LogLocal7, nil
	default:
		return conntrack.LogLocal0, fmt.Errorf("unsupported syslog facility %q", s)
	}
}
