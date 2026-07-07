package main

import (
	"context"
	"crypto/subtle"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/vponomarev/network-monitor/internal/bandwidth"
	"github.com/vponomarev/network-monitor/internal/collector"
	"github.com/vponomarev/network-monitor/internal/config"
	"github.com/vponomarev/network-monitor/internal/conntrack"
	"github.com/vponomarev/network-monitor/internal/discovery"
	"github.com/vponomarev/network-monitor/internal/dns"
	"github.com/vponomarev/network-monitor/internal/health"
	"github.com/vponomarev/network-monitor/internal/latency"
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
	enableTracing := flag.Bool("enable-tracing", false, "Automatically enable TCP retransmit tracing (requires root)")
	configPath := flag.String("config", "", "Path to configuration file (overrides NETMON_CONFIG env var)")
	flag.Parse()

	// Load configuration
	if *configPath == "" {
		*configPath = os.Getenv("NETMON_CONFIG")
		if *configPath == "" {
			*configPath = "config.yaml"
		}
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Enable tracing if requested
	if *enableTracing {
		logger, _ := zap.NewDevelopment() // Temporary logger for early messages
		logger.Info("Enabling TCP retransmit tracing",
			zap.String("path", collector.GetTracepointEnablePath()))
		if err := collector.EnableTracepoint(collector.GetTracepointEnablePath()); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to enable tracing: %v\n", err)
			os.Exit(1)
		}
		logger.Info("TCP retransmit tracing enabled successfully")
	}

	// Initialize logger
	logger, err := initLogger(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	// Check if tracing is enabled (warn if not)
	collector.CheckAndWarnTracepoint(logger, collector.GetTracepointEnablePath())

	logger.Info("Starting Network Monitor",
		zap.String("version", Version),
		zap.String("config", *configPath),
	)

	// Initialize metadata matchers (from local files - required for startup)
	locationMatcher, err := metadata.NewLocationMatcher(cfg.Metadata.Locations.Path, logger)
	if err != nil {
		logger.Warn("Failed to load locations, using empty matcher", zap.Error(err))
		locationMatcher = metadata.NewEmptyLocationMatcher(logger)
	} else {
		logger.Info("Locations loaded",
			zap.String("path", cfg.Metadata.Locations.Path),
			zap.Int("count", locationMatcher.Count()))
	}

	roleMatcher, err := metadata.NewRoleMatcher(cfg.Metadata.Roles.Path, logger)
	if err != nil {
		logger.Warn("Failed to load roles, using empty matcher", zap.Error(err))
		roleMatcher = metadata.NewEmptyRoleMatcher(logger)
	} else {
		logger.Info("Roles loaded",
			zap.String("path", cfg.Metadata.Roles.Path),
			zap.Int("count", roleMatcher.Count()))
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

	// Fatal-error propagation for CRITICAL components (loss collector, HTTP
	// server). When a critical component dies, we record the first error, cancel
	// the context to trigger graceful shutdown, and exit non-zero at the end so
	// a supervisor (systemd Restart=on-failure) restarts us. Non-critical
	// components (conntrack, bandwidth, latency, dns, discovery, pollers) only
	// log and never call setFatal.
	var (
		fatalMu  sync.Mutex
		fatalErr error
	)
	setFatal := func(err error) {
		fatalMu.Lock()
		if fatalErr == nil {
			fatalErr = err
		}
		fatalMu.Unlock()
		cancel()
	}

	// Create metrics exporter with topology and cardinality config.
	exporter := metrics.NewExporterWithConfig(
		cfg.Metrics.Name, locationMatcher, roleMatcher, logger,
		prometheus.DefaultRegisterer,
		metrics.CardinalityConfig{
			Level:     cfg.Metrics.Cardinality.Level,
			MaxSeries: cfg.Metrics.Cardinality.MaxSeries,
		},
	)
	exporter.SetTopology(networkTopology)
	// Periodic TTL cleanup: the raw CounterVec is registered (not the exporter),
	// so scrapes never trigger Collect/cleanupOld — run a janitor instead.
	exporter.StartJanitor(ctx, time.Minute)

	// Create metadata status API
	metadataAPI := metadata.NewMetadataStatusAPI()
	metadataAPI.RegisterCounter("locations", locationMatcher)
	metadataAPI.RegisterCounter("roles", roleMatcher)
	// Topology uses DeviceCount() instead of Count(), wrap it
	metadataAPI.RegisterCounter("topology", &topologyCounter{topology: networkTopology})

	// Start HTTP pollers for metadata updates (if configured)
	// Each poller runs independently and updates its file periodically
	// First poll happens 30 seconds after startup, then at configured intervals
	if cfg.Metadata.Locations.UpdateSource != nil {
		locationsPoller := metadata.NewHTTPPoller(
			metadata.HTTPPollerConfig{
				Name:     "locations",
				URL:      cfg.Metadata.Locations.UpdateSource.URL,
				Interval: cfg.Metadata.Locations.UpdateSource.PollIntervalDuration(),
				Timeout:  cfg.Metadata.Locations.UpdateSource.TimeoutDuration(),
				FilePath: cfg.Metadata.Locations.Path,
			},
			logger,
			prometheus.DefaultRegisterer,
		)
		locationsPoller.SetValidator(metadata.LocationsValidator)
		locationsPoller.SetReloadFunc(func() error {
			return locationMatcher.Reload(cfg.Metadata.Locations.Path)
		})
		metadataAPI.RegisterPoller("locations", locationsPoller)
		go locationsPoller.Run(ctx)

		logger.Info("HTTP update source enabled for locations",
			zap.String("url", cfg.Metadata.Locations.UpdateSource.URL),
			zap.Duration("interval", cfg.Metadata.Locations.UpdateSource.PollIntervalDuration()))
	}

	if cfg.Metadata.Roles.UpdateSource != nil {
		rolesPoller := metadata.NewHTTPPoller(
			metadata.HTTPPollerConfig{
				Name:     "roles",
				URL:      cfg.Metadata.Roles.UpdateSource.URL,
				Interval: cfg.Metadata.Roles.UpdateSource.PollIntervalDuration(),
				Timeout:  cfg.Metadata.Roles.UpdateSource.TimeoutDuration(),
				FilePath: cfg.Metadata.Roles.Path,
			},
			logger,
			prometheus.DefaultRegisterer,
		)
		rolesPoller.SetValidator(metadata.RolesValidator)
		rolesPoller.SetReloadFunc(func() error {
			return roleMatcher.Reload(cfg.Metadata.Roles.Path)
		})
		metadataAPI.RegisterPoller("roles", rolesPoller)
		go rolesPoller.Run(ctx)

		logger.Info("HTTP update source enabled for roles",
			zap.String("url", cfg.Metadata.Roles.UpdateSource.URL),
			zap.Duration("interval", cfg.Metadata.Roles.UpdateSource.PollIntervalDuration()))
	}

	if cfg.Metadata.Topology.UpdateSource != nil && cfg.Topology.Enabled {
		topologyPoller := metadata.NewHTTPPoller(
			metadata.HTTPPollerConfig{
				Name:     "topology",
				URL:      cfg.Metadata.Topology.UpdateSource.URL,
				Interval: cfg.Metadata.Topology.UpdateSource.PollIntervalDuration(),
				Timeout:  cfg.Metadata.Topology.UpdateSource.TimeoutDuration(),
				FilePath: cfg.Metadata.Topology.Path,
			},
			logger,
			prometheus.DefaultRegisterer,
		)
		topologyPoller.SetValidator(metadata.TopologyValidator)
		topologyPoller.SetReloadFunc(func() error {
			return networkTopology.Reload(cfg.Metadata.Topology.Path)
		})
		metadataAPI.RegisterPoller("topology", topologyPoller)
		go topologyPoller.Run(ctx)

		logger.Info("HTTP update source enabled for topology",
			zap.String("url", cfg.Metadata.Topology.UpdateSource.URL),
			zap.Duration("interval", cfg.Metadata.Topology.UpdateSource.PollIntervalDuration()))
	}

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	// Create reload channel for SIGHUP
	reloadChan := make(chan struct{}, 1)

	// Signal handler goroutine - runs for the entire lifetime
	go func() {
		for sig := range sigChan {
			switch sig {
			case syscall.SIGHUP:
				logger.Info("SIGHUP received, reloading configuration")
				select {
				case reloadChan <- struct{}{}:
				default:
					// Reload already pending
				}
				// Continue listening for more signals
			default:
				logger.Info("Received signal, shutting down", zap.String("signal", sig.String()))
				cancel()
				return
			}
		}
	}()

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

	// Readiness state: /ready reports 200 only once the loss collector is
	// actually consuming events, and flips back to 503 when it stops.
	healthState := health.NewState()

	// Create collector self-metrics
	collectorMetrics := collector.NewCollectorMetrics(prometheus.DefaultRegisterer, logger)

	// Start trace pipe collector
	collector := collector.NewTracePipeCollector(cfg.Global.TracePipePath, exporter, logger, collectorMetrics)
	collector.SetReadyFunc(func() {
		collectorMetrics.SetUp(true)
		healthState.SetCollectorReady(true)
	})
	go func() {
		err := collector.Run(ctx)
		// Collector stopped (error or shutdown): no longer collecting data.
		collectorMetrics.SetUp(false)
		healthState.SetCollectorReady(false)
		if err != nil && err != context.Canceled {
			// The loss collector is critical — its failure must take down the
			// process so it gets restarted rather than running blind.
			logger.Error("FATAL: loss collector stopped", zap.Error(err))
			setFatal(err)
		}
	}()

	// Start connection tracker (Linux only)
	var connTracker *conntrack.Tracker
	if cfg.Connections.Enabled {
		connCfg := conntrack.Config{
			TrackIncoming:   cfg.Connections.TrackIncoming,
			TrackOutgoing:   cfg.Connections.TrackOutgoing,
			TrackCloses:     true,
			FilterPorts:     cfg.Connections.FilterPorts,
			SYNTimeout:      30 * time.Second,
			EventBufferSize: cfg.Connections.EventBufferSize,
		}

		var err error
		connTracker, err = conntrack.NewTracker(connCfg, logger)
		if err != nil {
			logger.Warn("Failed to create connection tracker", zap.Error(err))
		} else {
			go func() {
				if err := connTracker.Run(ctx); err != nil {
					logger.Error("Connection tracker error", zap.Error(err))
				}
			}()
			logger.Info("Connection tracker started",
				zap.Bool("incoming", cfg.Connections.TrackIncoming),
				zap.Bool("outgoing", cfg.Connections.TrackOutgoing),
				zap.Int("buffer_size", connCfg.EventBufferSize))
		}
	}

	// Start bandwidth monitor (optional)
	if cfg.Bandwidth.Enabled {
		bwMonitor := bandwidth.NewMonitor(cfg.Bandwidth, logger)
		go func() {
			if err := bwMonitor.Run(ctx); err != nil {
				logger.Error("Bandwidth monitor error", zap.Error(err))
			}
		}()
		logger.Info("Bandwidth monitor started",
			zap.Strings("interfaces", cfg.Bandwidth.Interfaces),
			zap.Duration("interval", cfg.Bandwidth.IntervalDuration()))
	}

	// Start latency monitor (optional)
	if cfg.Latency.Enabled {
		latencyMonitor := latency.NewMonitor(cfg.Latency, logger)
		go func() {
			if err := latencyMonitor.Run(ctx); err != nil {
				logger.Error("Latency monitor error", zap.Error(err))
			}
		}()
		logger.Info("Latency monitor started",
			zap.Strings("targets", cfg.Latency.Targets),
			zap.Duration("interval", cfg.Latency.IntervalDuration()))
	}

	// Start DNS monitor (optional)
	if cfg.DNS.Enabled {
		dnsMonitor := dns.NewMonitor(cfg.DNS, logger)
		go func() {
			if err := dnsMonitor.Run(ctx); err != nil {
				logger.Error("DNS monitor error", zap.Error(err))
			}
		}()
		logger.Info("DNS monitor started",
			zap.Strings("interfaces", cfg.DNS.Interfaces),
			zap.Int("port", cfg.DNS.Port),
			zap.Duration("interval", cfg.DNS.IntervalDuration()))
	}

	// Start HTTP server for metrics and API
	mux := http.NewServeMux()

	// Helper to wrap handlers with optional auth
	requireAuth := func(handler http.Handler) http.Handler {
		if cfg.Global.AuthToken == "" {
			return handler
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Use subtle constant-time comparison to prevent timing attacks
			token := r.Header.Get("Authorization")
			if token == "" {
				token = r.URL.Query().Get("token")
			}
			if subtle.ConstantTimeCompare([]byte(token), []byte("Bearer "+cfg.Global.AuthToken)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			handler.ServeHTTP(w, r)
		})
	}

	// Prometheus metrics endpoint (protected if auth token is set)
	metricsHandler := promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{
			EnableOpenMetrics: true,
		},
	)
	mux.Handle("/metrics", requireAuth(metricsHandler))

	// Health and ready endpoints (NEVER protected by auth).
	// /health is liveness (always 200 while serving); /ready is readiness
	// (200 only when the loss collector is running, else 503).
	mux.HandleFunc("/health", healthState.LivenessHandler())
	mux.HandleFunc("/ready", healthState.ReadinessHandler())

	// Discovery API endpoints (protected if auth token is set)
	if discoveryService != nil {
		discoveryMux := discoveryService.HTTPHandler()
		mux.Handle("/api/v1/discover", requireAuth(discoveryMux))
		mux.Handle("/api/v1/loss/top", requireAuth(discoveryMux))

		logger.Info("Discovery API enabled",
			zap.String("endpoints", "/api/v1/discover, /api/v1/loss/top"))
	}

	// Metadata status API endpoint (protected if auth token is set)
	mux.Handle("/api/v1/metadata/", requireAuth(metadataAPI.HTTPHandler()))
	logger.Info("Metadata API enabled",
		zap.String("endpoint", "/api/v1/metadata/status"))

	// Connection tracking API endpoints (protected if auth token is set)
	if connTracker != nil {
		connAPI := conntrack.NewAPI(connTracker)
		connMux := connAPI.HTTPHandler()
		mux.Handle("/api/v1/conntrack/", requireAuth(connMux))

		logger.Info("Connection tracking API enabled",
			zap.String("endpoints", "/api/v1/conntrack/connections, /api/v1/conntrack/stats"))
	}

	server := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Global.MetricsAddr, cfg.Global.MetricsPort),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("Starting HTTP server", zap.Int("port", cfg.Global.MetricsPort))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// The HTTP server (metrics + API) is critical — bind failures etc.
			// must take down the process.
			logger.Error("FATAL: HTTP server error", zap.Error(err))
			setFatal(err)
		}
	}()

	logger.Info("Network Monitor started",
		zap.Int("port", cfg.Global.MetricsPort),
		zap.String("trace_pipe", cfg.Global.TracePipePath),
		zap.Bool("discovery", cfg.Discovery.Traceroute.Enabled),
		zap.Bool("bandwidth", cfg.Bandwidth.Enabled),
		zap.Bool("latency", cfg.Latency.Enabled),
		zap.Bool("dns", cfg.DNS.Enabled))

	// Configuration reload loop
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-reloadChan:
				logger.Info("Reloading configuration")

				// Reload config file
				newCfg, err := config.Load(*configPath)
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

	// If a critical component failed, exit non-zero so a supervisor restarts us.
	// os.Exit skips deferred logger.Sync(), so flush explicitly first.
	fatalMu.Lock()
	fe := fatalErr
	fatalMu.Unlock()
	if fe != nil {
		logger.Error("Exiting with non-zero status due to fatal component failure", zap.Error(fe))
		_ = logger.Sync()
		os.Exit(1)
	}
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

// topologyCounter wraps topology.Topology to implement CounterProvider
type topologyCounter struct {
	topology *topology.Topology
}

func (t *topologyCounter) Count() int {
	if t.topology == nil {
		return 0
	}
	return t.topology.DeviceCount()
}
