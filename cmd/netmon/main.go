package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/vponomarev/network-monitor/internal/collector"
	"github.com/vponomarev/network-monitor/internal/config"
	"github.com/vponomarev/network-monitor/internal/discovery"
	"github.com/vponomarev/network-monitor/internal/metadata"
	"github.com/vponomarev/network-monitor/internal/metrics"
	"github.com/vponomarev/network-monitor/internal/topology"
	"go.uber.org/zap"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

func main() {
	// Parse command line flags
	configPath := os.Getenv("NETMON_CONFIG")
	if configPath == "" {
		configPath = "config.yaml"
	}

	// Load configuration
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	logger, err := initLogger(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("Starting Network Monitor",
		zap.String("version", Version),
		zap.String("config", configPath),
	)

	// Initialize metadata matchers
	locationMatcher, err := metadata.NewLocationMatcher(cfg.Metadata.Locations.Path)
	if err != nil {
		logger.Warn("Failed to load locations, using empty matcher", zap.Error(err))
		locationMatcher = metadata.NewEmptyLocationMatcher()
	}

	roleMatcher, err := metadata.NewRoleMatcher(cfg.Metadata.Roles.Path)
	if err != nil {
		logger.Warn("Failed to load roles, using empty matcher", zap.Error(err))
		roleMatcher = metadata.NewEmptyRoleMatcher()
	}

	// Initialize topology (optional)
	var networkTopology *topology.Topology
	if cfg.Topology.Enabled {
		networkTopology, err = topology.Load(cfg.Topology.Path)
		if err != nil {
			logger.Warn("Failed to load topology, using empty topology", zap.Error(err))
			networkTopology = topology.NewTopology()
		} else {
			logger.Info("Topology loaded",
				zap.Int("devices", networkTopology.DeviceCount()),
				zap.String("type", networkTopology.GetTopologyType()))
		}
	} else {
		networkTopology = topology.NewTopology()
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	// Create reload channel for SIGHUP
	reloadChan := make(chan struct{}, 1)

	go func() {
		sig := <-sigChan
		switch sig {
		case syscall.SIGHUP:
			logger.Info("SIGHUP received, reloading configuration")
			select {
			case reloadChan <- struct{}{}:
			default:
				// Reload already pending
			}
			// Re-arm signal handler
			signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
		default:
			logger.Info("Received signal, shutting down", zap.String("signal", sig.String()))
			cancel()
		}
	}()

	// Create metrics exporter with topology
	exporter := metrics.NewExporter(cfg.Metrics.Name, locationMatcher, roleMatcher, logger)
	exporter.SetTopology(networkTopology)

	// Create discovery service with traceroute
	var discoveryService *discovery.DiscoveryService

	if cfg.Discovery.Traceroute.Enabled {
		// Parse interval
		interval, err := time.ParseDuration(cfg.Discovery.Traceroute.Interval)
		if err != nil {
			logger.Warn("Invalid traceroute interval, using default",
				zap.String("interval", cfg.Discovery.Traceroute.Interval),
				zap.Duration("default", 5*time.Minute))
			interval = 5 * time.Minute
		}

		// Create cache and loss tracker
		cache := discovery.NewPathCache(cfg.TTL(), 1000)
		lossTracker := discovery.NewLossTracker(cfg.TTL())

		// Create discovery service with default tracerouter
		discoveryService = discovery.NewDiscoveryService(
			discovery.NewDefaultTracerouter(),
			cache,
			lossTracker,
			cfg.Discovery.Traceroute.TopN,
			cfg.Discovery.Traceroute.Mode,
			interval,
		)

		logger.Info("Discovery service initialized",
			zap.Int("top_n", cfg.Discovery.Traceroute.TopN),
			zap.String("mode", cfg.Discovery.Traceroute.Mode))
	}

	// Start trace pipe collector
	collector := collector.NewTracePipeCollector(cfg.Global.TracePipePath, exporter, logger)
	go func() {
		if err := collector.Run(ctx); err != nil {
			logger.Error("Collector error", zap.Error(err))
		}
	}()

	// Start HTTP server for metrics and API
	mux := http.NewServeMux()

	// Prometheus metrics endpoint
	mux.Handle("/metrics", promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{
			EnableOpenMetrics: true,
		},
	))

	// Health and ready endpoints
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Discovery API endpoints
	if discoveryService != nil {
		discoveryMux := discoveryService.HTTPHandler()
		mux.Handle("/api/v1/discover", discoveryMux)
		mux.Handle("/api/v1/loss/top", discoveryMux)

		logger.Info("Discovery API enabled",
			zap.String("endpoints", "/api/v1/discover, /api/v1/loss/top"))
	}

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Global.MetricsPort),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("Starting HTTP server", zap.Int("port", cfg.Global.MetricsPort))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP server error", zap.Error(err))
			cancel()
		}
	}()

	logger.Info("Network Monitor started",
		zap.Int("port", cfg.Global.MetricsPort),
		zap.String("trace_pipe", cfg.Global.TracePipePath),
		zap.Bool("discovery", cfg.Discovery.Traceroute.Enabled))

	// Configuration reload loop
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-reloadChan:
				logger.Info("Reloading configuration")

				// Reload config file
				newCfg, err := config.Load(configPath)
				if err != nil {
					logger.Error("Failed to reload config", zap.Error(err))
					continue
				}

				// Reload metadata matchers
				if err := locationMatcher.Reload(newCfg.Metadata.Locations.Path); err != nil {
					logger.Warn("Failed to reload locations", zap.Error(err))
				} else {
					logger.Info("Locations reloaded")
				}

				if err := roleMatcher.Reload(newCfg.Metadata.Roles.Path); err != nil {
					logger.Warn("Failed to reload roles", zap.Error(err))
				} else {
					logger.Info("Roles reloaded")
				}

				// Reload topology if enabled
				if newCfg.Topology.Enabled {
					if err := networkTopology.Reload(newCfg.Topology.Path); err != nil {
						logger.Warn("Failed to reload topology", zap.Error(err))
					} else {
						logger.Info("Topology reloaded",
							zap.Int("devices", networkTopology.DeviceCount()))
					}
				}

				// Update exporter with new matchers and topology
				exporter.SetMatchers(locationMatcher, roleMatcher)
				exporter.SetTopology(networkTopology)

				logger.Info("Configuration reloaded successfully")
			}
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()

	// Graceful shutdown
	logger.Info("Shutting down...")

	// Stop discovery service
	if discoveryService != nil {
		discoveryService.Stop()
	}

	// Shutdown HTTP server
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("Server shutdown error", zap.Error(err))
	}

	logger.Info("Network Monitor stopped")
}

func initLogger(cfg *config.Config) (*zap.Logger, error) {
	var zapCfg zap.Config

	switch cfg.Logging.Format {
	case "json":
		zapCfg = zap.NewProductionConfig()
	default:
		zapCfg = zap.NewDevelopmentConfig()
	}

	switch cfg.Logging.Level {
	case "debug":
		zapCfg.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
	case "info":
		zapCfg.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	case "warn":
		zapCfg.Level = zap.NewAtomicLevelAt(zap.WarnLevel)
	case "error":
		zapCfg.Level = zap.NewAtomicLevelAt(zap.ErrorLevel)
	}

	return zapCfg.Build()
}
