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
	"github.com/vponomarev/network-monitor/internal/packetloss"
	"go.uber.org/zap"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"

	interfaceName string
	configFile    string
)

func main() {
	rootCmd := &cobra.Command{
		Use:     "pktloss",
		Short:   "Packet Loss Monitor",
		Long:    "Monitors network packet loss using /sys/kernel/tracing/trace_pipe (Linux only)",
		Version: Version,
		RunE:    run,
	}

	rootCmd.Flags().StringVarP(&interfaceName, "interface", "i", "", "Network interface to monitor")
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

	// Override interface if provided via flag
	if interfaceName != "" {
		cfg.Monitoring.PacketLoss.Interfaces = []string{interfaceName}
	}

	// Initialize logger
	logger, err := initLogger(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	defer logger.Sync()

	logger.Info("Starting Packet Loss Monitor",
		zap.String("version", Version),
		zap.Strings("interfaces", cfg.Monitoring.PacketLoss.Interfaces),
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

	// Start packet loss monitor
	monitor := packetloss.NewMonitor(cfg.Monitoring.PacketLoss, logger)
	if err := monitor.Run(ctx); err != nil {
		return fmt.Errorf("packet loss monitor error: %w", err)
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
