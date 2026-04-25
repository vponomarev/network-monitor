//go:build linux
// +build linux

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/vponomarev/network-monitor/internal/config"
	"github.com/vponomarev/network-monitor/internal/conntrack"
	"go.uber.org/zap"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"

	ebpfProgram string
	configFile  string
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

	// Initialize connection tracker
	tracker, err := conntrack.NewTracker(conntrack.Config{
		EBPFProgramPath: ebpfProgram,
		TrackIncoming:   cfg.Connections.TrackIncoming,
		TrackOutgoing:   cfg.Connections.TrackOutgoing,
	}, logger)
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
