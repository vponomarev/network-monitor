//go:build linux
// +build linux

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/vponomarev/network-monitor/internal/config"
	"github.com/vponomarev/network-monitor/internal/conntrack"
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
)

func main() {
	rootCmd := &cobra.Command{
		Use:     "conntrack",
		Short:   "Connection Tracker",
		Long:    "eBPF-based connection tracking for incoming and outgoing network connections (Linux only)",
		Version: Version,
		RunE:    run,
	}

	rootCmd.Flags().StringVarP(&ebpfProgram, "ebpf-prog", "p", "", "Path to eBPF program object file")
	rootCmd.Flags().StringVarP(&configFile, "config", "c", "", "Config file path")

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
	defer logger.Sync()

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

	// Create tracker config
	ebpfPath := ebpfProgram
	if ebpfPath == "" {
		ebpfPath = conntrack.DefaultEBPFProgramPath
	}

	trackerCfg := conntrack.Config{
		EBPFProgramPath: ebpfPath,
		TrackIncoming:   trackIncoming && cfg.Connections.TrackIncoming,
		TrackOutgoing:   trackOutgoing && cfg.Connections.TrackOutgoing,
		TrackCloses:     trackCloses,
		FilterPorts:     cfg.Connections.FilterPorts,
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

	// Start tracking
	if err := tracker.Run(ctx); err != nil {
		return fmt.Errorf("connection tracker error: %w", err)
	}

	return nil
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
	return zapCfg.Build()
}

func checkPlatform() error {
	// This function only exists on Linux builds
	return nil
}

func parseSyslogFacility(s string) (conntrack.SyslogFacility, error) {
	switch s {
	case "USER", "user":
		return conntrack.LOG_USER, nil
	case "DAEMON", "daemon":
		return conntrack.LOG_DAEMON, nil
	case "LOCAL0", "local0":
		return conntrack.LOG_LOCAL0, nil
	case "LOCAL1", "local1":
		return conntrack.LOG_LOCAL1, nil
	case "LOCAL2", "local2":
		return conntrack.LOG_LOCAL2, nil
	case "LOCAL3", "local3":
		return conntrack.LOG_LOCAL3, nil
	case "LOCAL4", "local4":
		return conntrack.LOG_LOCAL4, nil
	case "LOCAL5", "local5":
		return conntrack.LOG_LOCAL5, nil
	case "LOCAL6", "local6":
		return conntrack.LOG_LOCAL6, nil
	case "LOCAL7", "local7":
		return conntrack.LOG_LOCAL7, nil
	default:
		return conntrack.LOG_LOCAL0, nil
	}
}
