package metadata

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// Validator функция валидации данных (type-specific)
type Validator func([]byte) error

// ReloadFunc функция перезагрузки в памяти (callback)
type ReloadFunc func() error

// HTTPPollerConfig конфигурация poller
type HTTPPollerConfig struct {
	Name     string        // "locations", "roles", "topology"
	URL      string        // HTTP endpoint для загрузки
	Interval time.Duration // Интервал между опросами
	Timeout  time.Duration // Timeout для HTTP запроса
	FilePath string        // Путь к локальному файлу для обновления
}

// HTTPPoller управляет периодическим обновлением из HTTP
type HTTPPoller struct {
	config    HTTPPollerConfig
	logger    *zap.Logger
	client    *http.Client
	validator Validator
	reload    ReloadFunc

	// Metrics
	updateCounter  prometheus.Counter
	updateErrors   prometheus.Counter
	lastUpdateTime prometheus.Gauge
	lastHashGauge  prometheus.Gauge

	mu        sync.RWMutex
	lastHash  string
	lastCheck time.Time
	success   bool
}

// NewHTTPPoller создаёт новый poller
func NewHTTPPoller(cfg HTTPPollerConfig, logger *zap.Logger, reg prometheus.Registerer) *HTTPPoller {
	poller := &HTTPPoller{
		config: cfg,
		logger: logger.Named("http-poller." + cfg.Name),
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}

	// Регистрация метрик
	if reg != nil {
		poller.updateCounter = prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "metadata_update_total",
			Help:        "Total number of successful metadata updates from HTTP",
			ConstLabels: prometheus.Labels{"source": cfg.Name},
		})

		poller.updateErrors = prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "metadata_update_errors_total",
			Help:        "Total number of metadata update errors from HTTP",
			ConstLabels: prometheus.Labels{"source": cfg.Name},
		})

		poller.lastUpdateTime = prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        "metadata_last_update_timestamp_seconds",
			Help:        "Timestamp of last successful metadata update from HTTP",
			ConstLabels: prometheus.Labels{"source": cfg.Name},
		})

		poller.lastHashGauge = prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        "metadata_last_hash",
			Help:        "Hash of last successfully loaded metadata",
			ConstLabels: prometheus.Labels{"source": cfg.Name},
		})

		reg.MustRegister(
			poller.updateCounter,
			poller.updateErrors,
			poller.lastUpdateTime,
			poller.lastHashGauge,
		)
	}

	return poller
}

// SetValidator устанавливает валидатор данных
func (p *HTTPPoller) SetValidator(v Validator) {
	p.validator = v
}

// SetReloadFunc устанавливает callback для reload в памяти
func (p *HTTPPoller) SetReloadFunc(fn ReloadFunc) {
	p.reload = fn
}

// Run запускает фоновый polling
// Первый poll происходит через 30 секунд после старта, затем каждые cfg.Interval
func (p *HTTPPoller) Run(ctx context.Context) {
	p.logger.Info("Starting HTTP poller",
		zap.String("url", p.config.URL),
		zap.Duration("interval", p.config.Interval))

	// Первый poll через 30 секунд после старта
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("Stopping HTTP poller")
			return
		case <-timer.C:
			p.checkAndUpdate(ctx)
			timer.Reset(p.config.Interval)
		}
	}
}

func (p *HTTPPoller) checkAndUpdate(ctx context.Context) {
	p.logger.Debug("Checking for updates", zap.String("url", p.config.URL))

	// Загрузка данных из HTTP
	data, err := p.fetch(ctx)
	if err != nil {
		p.logger.Warn("Failed to fetch from HTTP",
			zap.String("url", p.config.URL),
			zap.Error(err))

		if p.updateErrors != nil {
			p.updateErrors.Inc()
		}
		return
	}

	// Проверка на изменения (hash comparison)
	hash := fmt.Sprintf("%x", sha256.Sum256(data))

	p.mu.RLock()
	if hash == p.lastHash {
		p.mu.RUnlock()
		p.logger.Debug("No changes detected",
			zap.String("hash", hash[:8]))
		return
	}
	p.mu.RUnlock()

	p.logger.Info("Changes detected, validating",
		zap.String("url", p.config.URL),
		zap.String("old_hash", func() string {
			if len(p.lastHash) >= 8 {
				return p.lastHash[:8]
			}
			return "initial"
		}()),
		zap.String("new_hash", hash[:8]))

	// Валидация через type-specific валидатор
	if p.validator != nil {
		if err := p.validator(data); err != nil {
			p.logger.Error("Validation failed, skipping update",
				zap.String("url", p.config.URL),
				zap.Error(err))

			if p.updateErrors != nil {
				p.updateErrors.Inc()
			}
			return
		}
		p.logger.Debug("Validation passed", zap.String("url", p.config.URL))
	}

	// Атомарная запись в файл
	if err := p.atomicWrite(data); err != nil {
		p.logger.Error("Failed to write file",
			zap.String("path", p.config.FilePath),
			zap.Error(err))

		if p.updateErrors != nil {
			p.updateErrors.Inc()
		}
		return
	}

	p.logger.Debug("File written successfully",
		zap.String("path", p.config.FilePath))

	// Reload в памяти
	if p.reload != nil {
		if err := p.reload(); err != nil {
			p.logger.Error("Reload failed, file updated but memory not refreshed",
				zap.String("path", p.config.FilePath),
				zap.Error(err))
			// Не считаем это ошибкой update - файл уже записан
		} else {
			p.logger.Debug("Memory reload successful",
				zap.String("path", p.config.FilePath))
		}
	}

	// Обновление состояния
	p.mu.Lock()
	p.lastHash = hash
	p.lastCheck = time.Now()
	p.success = true
	p.mu.Unlock()

	// Метрики
	if p.updateCounter != nil {
		p.updateCounter.Inc()
	}
	if p.lastUpdateTime != nil {
		p.lastUpdateTime.Set(float64(time.Now().Unix()))
	}
	if p.lastHashGauge != nil {
		// Конвертируем первые 8 байт hash в float для метрики
		p.lastHashGauge.Set(hashFloat(hash))
	}

	p.logger.Info("Metadata updated successfully",
		zap.String("url", p.config.URL),
		zap.String("hash", hash[:8]),
		zap.String("path", p.config.FilePath))
}

func (p *HTTPPoller) fetch(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", p.config.URL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func (p *HTTPPoller) atomicWrite(data []byte) error {
	dir := filepath.Dir(p.config.FilePath)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmpPath := p.config.FilePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, p.config.FilePath); err != nil {
		return err
	}

	return nil
}

// GetStatus возвращает статус poller
func (p *HTTPPoller) GetStatus() (lastCheck time.Time, hash string, success bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastCheck, p.lastHash, p.success
}

// hashFloat конвертирует hash в float64 для метрики
func hashFloat(hash string) float64 {
	if len(hash) < 16 {
		return 0
	}
	var val float64
	fmt.Sscanf(hash[:16], "%x", &val)
	return val
}

// validateYAML базовая валидация YAML (используется как fallback)
func validateYAML(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("empty data")
	}

	var dummy interface{}
	if err := yaml.Unmarshal(data, &dummy); err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}

	return nil
}
