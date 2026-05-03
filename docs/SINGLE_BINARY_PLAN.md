# План: Single-Binary поставка Network Monitor (v3)

**Дата:** 3 мая 2026  
**Цель:** Упаковка приложения в единый бинарный файл со встроенными ресурсами и командами install/deinstall

---

## Подтверждённые требования

### Приоритет загрузки .o файла

```
1. --ebpf-prog <path>       (явно указан пользователем, наивысший приоритет)
2. Embedded версия          (по умолчанию, всегда)
3. Не используется          (build без embed, симуляция событий)
```

### Пути по умолчанию

| Компонент | Путь |
|-----------|------|
| Бинарник | `/usr/local/bin/conntrack` |
| eBPF программа | `/usr/share/conntrack/bpf/conntrack.bpf.o` |
| Config | `/etc/conntrack/config.yaml` |
| systemd unit | `/etc/systemd/system/conntrack.service` |

### Deinstall поведение

- **Config НЕ удаляется** (сохраняется для будущей установки)
- Удаляются: бинарник, eBPF, systemd unit

### Проверка прав

- **Проверка запускаемости** — да (проверка что можем записать/прочитать)
- **Проверка root** — нет (может быть root без прав на запись в конкретные пути)

### Логирование install/deinstall

- **stdout** — основной вывод для пользователя
- **syslog** — дублирование для аудита

---

## Новые команды и флаги

### Команды

| Команда | Описание |
|---------|----------|
| `run` (default) | Запуск приложения |
| `install` | Установка в систему |
| `deinstall` | Удаление из системы |
| `show-config` | Печать sample config |

### Флаги

| Флаг | Описание |
|------|----------|
| `--ebpf-prog <path>` | Путь к внешнему .o файлу (приоритет 1) |
| `--config <path>` | Путь к config файлу |
| `--show-config` | Печать sample config (альтернатива команде) |
| `--install-path <path>` | Кастомный путь установки (default: `/usr/local/bin`) |
| `--export-ebpf-prog <path>` | **НОВОЕ** — экспорт embedded .o в файл |

---

## Примеры использования

```bash
# Запуск с embedded eBPF (по умолчанию)
sudo conntrack --config /etc/conntrack/config.yaml

# Запуск с внешним .o файлом
sudo conntrack --ebpf-prog /path/to/conntrack.bpf.o --config /etc/conntrack/config.yaml

# Установка в систему
sudo conntrack install

# Установка в кастомный путь
sudo conntrack install --install-path /opt/bin

# Удаление (config сохраняется)
sudo conntrack deinstall

# Показать sample config
conntrack show-config
# или
conntrack --show-config

# Экспорт .o файла (для отладки или ручного использования)
conntrack --export-ebpf-prog ./conntrack.bpf.o
# или
sudo conntrack export-ebpf-prog --output /usr/share/conntrack/bpf/conntrack.bpf.o
```

---

## Архитектура

### Процесс сборки релиза

```
┌─────────────────────────────────────────────────────────────┐
│                    CI/CD Pipeline (GitHub Actions)           │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  1. Linux Runner (Ubuntu 22.04)                             │
│     ┌──────────────────────────────────────────────┐        │
│     │ a. Сборка eBPF:                              │        │
│     │    make -C bpf all                           │        │
│     │    → bpf/conntrack.bpf.o                     │        │
│     └──────────────────────────────────────────────┘        │
│                                                              │
│     ┌──────────────────────────────────────────────┐        │
│     │ b. Копирование в pkg/embedded/               │        │
│     │    cp bpf/conntrack.bpf.o pkg/embedded/bpf/  │        │
│     │    cp configs/*.yaml pkg/embedded/configs/   │        │
│     │    cp packaging/systemd/*.service            │        │
│     │       pkg/embedded/systemd/                  │        │
│     └──────────────────────────────────────────────┘        │
│                                                              │
│     ┌──────────────────────────────────────────────┐        │
│     │ c. Сборка Go binary с embed:                 │        │
│     │    go build -o conntrack ./cmd/conntrack     │        │
│     │    → единый бинарник ~15-20MB                │        │
│     └──────────────────────────────────────────────┘        │
│                                                              │
│  2. Артефакты релиза:                                       │
│     - conntrack-linux-amd64 (единый файл)                   │
│     - SHA256SUMS                                            │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

### Структура проекта

```
network-monitor/
├── cmd/
│   └── conntrack/
│       ├── main.go                  # Команды: run, install, deinstall, show-config
│       ├── install.go               # Логика install/deinstall
│       └── export.go                # Логика --export-ebpf-prog (новый файл)
├── pkg/
│   └── embedded/                     # НОВОЕ
│       ├── bpf/
│       │   └── conntrack.bpf.o      # Копируется при сборке
│       ├── configs/
│       │   └── config.example.yaml  # Sample config
│       └── systemd/
│           └── conntrack.service    # systemd unit
├── internal/
│   └── conntrack/
│       └── tracker_linux.go         # loadEBPF() с embed поддержкой
├── bpf/
│   └── conntrack.bpf.c              # Исходники eBPF
├── configs/
│   └── config.example.yaml          # Sample config (источник)
├── packaging/
│   └── systemd/
│       └── conntrack.service        # systemd unit (источник)
├── Makefile
└── .github/workflows/
    └── release.yml
```

---

## Изменения в коде

### 1. pkg/embedded/embed.go (новый файл)

```go
//go:build linux
// +build linux

package embedded

import (
    "embed"
    "fmt"
    "os"
    "path/filepath"
)

//go:embed bpf/conntrack.bpf.o
var ebpfData []byte

//go:embed configs/config.example.yaml
var configData []byte

//go:embed systemd/conntrack.service
var systemdUnitData []byte

// GetEBPFProgram возвращает .o файл как []byte
func GetEBPFProgram() ([]byte, error) {
    if len(ebpfData) == 0 {
        return nil, fmt.Errorf("embedded eBPF program not available (build without embed)")
    }
    return ebpfData, nil
}

// GetSampleConfig возвращает sample config как []byte
func GetSampleConfig() ([]byte, error) {
    if len(configData) == 0 {
        return nil, fmt.Errorf("embedded config not available")
    }
    return configData, nil
}

// GetSystemdUnit возвращает systemd unit файл как []byte
func GetSystemdUnit() ([]byte, error) {
    if len(systemdUnitData) == 0 {
        return nil, fmt.Errorf("embedded systemd unit not available")
    }
    return systemdUnitData, nil
}

// WriteEBPFToFile записывает eBPF программу в файл
func WriteEBPFToFile(path string) error {
    data, err := GetEBPFProgram()
    if err != nil {
        return err
    }
    
    // Создаём директорию
    dir := filepath.Dir(path)
    if err := os.MkdirAll(dir, 0755); err != nil {
        return fmt.Errorf("creating directory %s: %w", dir, err)
    }
    
    return os.WriteFile(path, data, 0644)
}

// WriteConfigToFile записывает config в файл
func WriteConfigToFile(path string) error {
    data, err := GetSampleConfig()
    if err != nil {
        return err
    }
    return os.WriteFile(path, data, 0644)
}

// WriteSystemdUnitToFile записывает systemd unit в файл
func WriteSystemdUnitToFile(path string) error {
    data, err := GetSystemdUnit()
    if err != nil {
        return err
    }
    return os.WriteFile(path, data, 0644)
}

// HasEmbeddedEBPF проверяет, доступна ли embedded версия
func HasEmbeddedEBPF() bool {
    return len(ebpfData) > 0
}

// ExportEBPFToFile экспортирует embedded .o в указанный файл
func ExportEBPFToFile(path string) error {
    data, err := GetEBPFProgram()
    if err != nil {
        return fmt.Errorf("getting embedded eBPF: %w", err)
    }
    
    // Создаём директорию
    dir := filepath.Dir(path)
    if err := os.MkdirAll(dir, 0755); err != nil {
        return fmt.Errorf("creating directory %s: %w", dir, err)
    }
    
    if err := os.WriteFile(path, data, 0644); err != nil {
        return fmt.Errorf("writing eBPF file: %w", err)
    }
    
    return nil
}
```

### 2. cmd/conntrack/main.go (обновления)

```go
package main

import (
    "fmt"
    "os"
    
    "github.com/spf13/cobra"
    "github.com/vponomarev/network-monitor/pkg/embedded"
    "go.uber.org/zap"
)

var (
    Version   = "dev"
    BuildTime = "unknown"
    GitCommit = "unknown"
    
    // Флаги
    ebpfProgram      string
    configFile       string
    showConfig       bool
    installPath      string
    exportEBPFPath   string
)

func main() {
    rootCmd := &cobra.Command{
        Use:     "conntrack",
        Short:   "Connection Tracker",
        Long:    "eBPF-based connection tracking (Linux only)",
        Version: Version,
        RunE:    run,
    }
    
    // Флаги
    rootCmd.Flags().StringVarP(&ebpfProgram, "ebpf-prog", "p", "", "Path to eBPF program object file")
    rootCmd.Flags().StringVarP(&configFile, "config", "c", "", "Config file path")
    rootCmd.Flags().BoolVar(&showConfig, "show-config", false, "Print sample configuration and exit")
    rootCmd.Flags().StringVar(&installPath, "install-path", "/usr/local/bin", "Installation path")
    rootCmd.Flags().StringVar(&exportEBPFPath, "export-ebpf-prog", "", "Export embedded eBPF program to file")
    
    // Команды
    rootCmd.AddCommand(installCmd)
    rootCmd.AddCommand(deinstallCmd)
    rootCmd.AddCommand(showConfigCmd)
    
    if err := rootCmd.Execute(); err != nil {
        os.Exit(1)
    }
}

// show-config команда
var showConfigCmd = &cobra.Command{
    Use:   "show-config",
    Short: "Print sample configuration",
    RunE: func(cmd *cobra.Command, args []string) error {
        data, err := embedded.GetSampleConfig()
        if err != nil {
            return fmt.Errorf("failed to get sample config: %w", err)
        }
        fmt.Println(string(data))
        return nil
    },
}

// install команда
var installCmd = &cobra.Command{
    Use:   "install",
    Short: "Install conntrack to system",
    Long:  "Install conntrack binary, eBPF program, systemd unit and configuration",
    RunE:  runInstall,
}

// deinstall команда
var deinstallCmd = &cobra.Command{
    Use:   "deinstall",
    Short: "Remove conntrack from system",
    Long:  "Remove conntrack binary, eBPF program, systemd unit (config is preserved)",
    RunE:  runDeinstall,
}

func run(cmd *cobra.Command, args []string) error {
    // Обработка --show-config (флаг)
    if showConfig {
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
    
    // Загрузка конфигурации
    cfg, err := config.Load(configFile)
    if err != nil {
        return fmt.Errorf("failed to load config: %w", err)
    }
    
    // Инициализация логгера
    logger, err := initLogger(cfg)
    if err != nil {
        return fmt.Errorf("failed to initialize logger: %w", err)
    }
    defer logger.Sync()
    
    logger.Info("Starting Connection Tracker",
        zap.String("version", Version),
        zap.String("ebpf_program", ebpfProgram),
        zap.Bool("embedded_ebpf", embedded.HasEmbeddedEBPF()),
    )
    
    // ... остальной код запуска
}
```

### 3. cmd/conntrack/install.go (обновления)

```go
//go:build linux
// +build linux

package main

import (
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "syslog"
    
    "github.com/vponomarev/network-monitor/pkg/embedded"
)

const (
    defaultInstallPath   = "/usr/local/bin"
    defaultEBPFPath      = "/usr/share/conntrack/bpf/conntrack.bpf.o"
    defaultConfigPath    = "/etc/conntrack/config.yaml"
    defaultSystemdPath   = "/etc/systemd/system/conntrack.service"
)

// syslogWriter для дублирования логов
var syslogWriter *syslog.Writer

func initSyslog() {
    w, err := syslog.New(syslog.LOG_INFO, "conntrack-install")
    if err == nil {
        syslogWriter = w
    }
}

func logInfo(format string, args ...interface{}) {
    msg := fmt.Sprintf(format, args...)
    fmt.Println(msg)
    if syslogWriter != nil {
        syslogWriter.Info(msg)
    }
}

func logError(format string, args ...interface{}) {
    msg := fmt.Sprintf(format, args...)
    fmt.Fprintf(os.Stderr, "%s\n", msg)
    if syslogWriter != nil {
        syslogWriter.Err(msg)
    }
}

func runInstall(cmd *cobra.Command, args []string) error {
    initSyslog()
    defer func() {
        if syslogWriter != nil {
            syslogWriter.Close()
        }
    }()
    
    installPath := cmd.Flag("install-path").Value.String()
    if installPath == "" {
        installPath = defaultInstallPath
    }
    
    binaryPath := filepath.Join(installPath, "conntrack")
    
    logInfo("Installing conntrack...")
    logInfo("Installation path: %s", installPath)
    
    // Проверка запускаемости (проверка прав на запись)
    if err := checkWritePermissions(installPath); err != nil {
        return fmt.Errorf("permission check failed: %w", err)
    }
    
    // 1. Установка бинарника
    if err := installBinary(binaryPath); err != nil {
        logError("Failed to install binary: %v", err)
        return fmt.Errorf("installing binary: %w", err)
    }
    logInfo("✓ Installed binary: %s", binaryPath)
    
    // 2. Установка eBPF программы
    if err := embedded.WriteEBPFToFile(defaultEBPFPath); err != nil {
        logError("Failed to install eBPF program: %v", err)
        return fmt.Errorf("installing eBPF program: %w", err)
    }
    logInfo("✓ Installed eBPF program: %s", defaultEBPFPath)
    
    // 3. Установка config (если не существует)
    if _, err := os.Stat(defaultConfigPath); err == nil {
        logInfo("⚠ Config already exists: %s (skipped)", defaultConfigPath)
    } else if os.IsNotExist(err) {
        if err := embedded.WriteConfigToFile(defaultConfigPath); err != nil {
            logError("Failed to install config: %v", err)
            return fmt.Errorf("installing config: %w", err)
        }
        logInfo("✓ Installed config: %s", defaultConfigPath)
    } else {
        logError("Failed to check config: %v", err)
        return fmt.Errorf("checking config: %w", err)
    }
    
    // 4. Установка systemd unit
    if err := embedded.WriteSystemdUnitToFile(defaultSystemdPath); err != nil {
        logError("Failed to install systemd unit: %v", err)
        return fmt.Errorf("installing systemd unit: %w", err)
    }
    logInfo("✓ Installed systemd unit: %s", defaultSystemdPath)
    
    // 5. Reload systemd
    if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
        logInfo("⚠ Failed to reload systemd: %v", err)
        logInfo("  Run 'sudo systemctl daemon-reload' manually")
    } else {
        logInfo("✓ Reloaded systemd daemon")
    }
    
    logInfo("")
    logInfo("✓ Installation complete!")
    logInfo("")
    logInfo("To start conntrack:")
    logInfo("  sudo systemctl enable conntrack")
    logInfo("  sudo systemctl start conntrack")
    logInfo("")
    logInfo("To view logs:")
    logInfo("  sudo journalctl -u conntrack -f")
    
    return nil
}

func runDeinstall(cmd *cobra.Command, args []string) error {
    initSyslog()
    defer func() {
        if syslogWriter != nil {
            syslogWriter.Close()
        }
    }()
    
    logInfo("Deinstalling conntrack...")
    
    // 1. Остановка сервиса
    exec.Command("systemctl", "stop", "conntrack").Run()
    exec.Command("systemctl", "disable", "conntrack").Run()
    logInfo("✓ Stopped and disabled systemd service")
    
    // 2. Reload systemd
    exec.Command("systemctl", "daemon-reload").Run()
    
    // 3. Удаление файлов (config НЕ удаляется)
    files := []struct {
        path string
        name string
    }{
        {defaultInstallPath + "/conntrack", "binary"},
        {defaultEBPFPath, "eBPF program"},
        {defaultSystemdPath, "systemd unit"},
    }
    
    for _, f := range files {
        if err := os.Remove(f.path); err != nil {
            if os.IsNotExist(err) {
                logInfo("⚠ File not found: %s (skipped)", f.path)
            } else {
                logError("Failed to remove %s: %v", f.name, err)
            }
        } else {
            logInfo("✓ Removed %s: %s", f.name, f.path)
        }
    }
    
    // 4. Удаление директории eBPF если пуста
    os.RemoveAll("/usr/share/conntrack")
    
    // 5. Config НЕ удаляется
    logInfo("✓ Preserved config: %s", defaultConfigPath)
    
    logInfo("")
    logInfo("✓ Deinstallation complete!")
    
    return nil
}

func checkWritePermissions(dir string) error {
    // Проверяем возможность записи в директорию
    testFile := filepath.Join(dir, ".conntrack-install-test")
    if err := os.WriteFile(testFile, []byte(""), 0644); err != nil {
        return fmt.Errorf("cannot write to %s: %w", dir, err)
    }
    os.Remove(testFile)
    return nil
}

func installBinary(destPath string) error {
    // Получаем путь к текущему бинарнику
    srcPath, err := os.Executable()
    if err != nil {
        return fmt.Errorf("getting executable path: %w", err)
    }
    
    // Создаём директорию
    dir := filepath.Dir(destPath)
    if err := os.MkdirAll(dir, 0755); err != nil {
        return fmt.Errorf("creating directory: %w", err)
    }
    
    // Копируем файл
    srcData, err := os.ReadFile(srcPath)
    if err != nil {
        return fmt.Errorf("reading source binary: %w", err)
    }
    
    if err := os.WriteFile(destPath, srcData, 0755); err != nil {
        return fmt.Errorf("writing destination: %w", err)
    }
    
    return nil
}
```

### 4. internal/conntrack/tracker_linux.go (обновления)

```go
func (t *Tracker) loadEBPF() error {
    // Приоритет 1: явно указан путь через флаг
    if t.config.EBPFProgramPath != "" {
        t.logger.Info("Loading eBPF from specified path", 
            zap.String("path", t.config.EBPFProgramPath))
        return t.loadEBPFFromFile(t.config.EBPFProgramPath)
    }
    
    // Приоритет 2: embedded версия (всегда используется по умолчанию)
    if embedded.HasEmbeddedEBPF() {
        t.logger.Info("Using embedded eBPF program")
        return t.loadEmbeddedEBPF()
    }
    
    // Приоритет 3: build без embed (симуляция)
    t.logger.Info("No eBPF program available, using simulated events")
    go t.simulateEvents()
    return nil
}

func (t *Tracker) loadEmbeddedEBPF() error {
    // Создаём временный файл
    tmpFile, err := os.CreateTemp("", "conntrack-ebpf-*.o")
    if err != nil {
        return fmt.Errorf("creating temp file: %w", err)
    }
    defer os.Remove(tmpFile.Name())
    
    data, err := embedded.GetEBPFProgram()
    if err != nil {
        return fmt.Errorf("getting embedded eBPF: %w", err)
    }
    
    if _, err := tmpFile.Write(data); err != nil {
        return fmt.Errorf("writing eBPF: %w", err)
    }
    tmpFile.Close()
    
    return t.loadEBPFFromFile(tmpFile.Name())
}

func (t *Tracker) loadEBPFFromFile(path string) error {
    spec, err := ebpf.LoadCollectionSpec(path)
    if err != nil {
        return fmt.Errorf("loading collection spec from %s: %w", path, err)
    }
    
    // ... остальной код
}
```

---

## systemd unit файл

**packaging/systemd/conntrack.service:**

```ini
[Unit]
Description=Network Connection Tracker (eBPF)
After=network.target
Documentation=man:conntrack(1)

[Service]
Type=simple
ExecStart=/usr/local/bin/conntrack --config /etc/conntrack/config.yaml
Restart=on-failure
RestartSec=5

# Capabilities required for eBPF
AmbientCapabilities=CAP_BPF CAP_PERFMON CAP_NET_RAW CAP_SYS_ADMIN
CapabilityBoundingSet=CAP_BPF CAP_PERFMON CAP_NET_RAW CAP_SYS_ADMIN

# Security hardening
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
```

---

## Изменения в Makefile

```makefile
# =============================================================================
# Embedded ресурсы
# =============================================================================

## prepare-embedded: Подготовить embedded ресурсы
prepare-embedded:
	@echo "Preparing embedded resources..."
	@mkdir -p pkg/embedded/bpf pkg/embedded/configs pkg/embedded/systemd
	@cp bpf/conntrack.bpf.o pkg/embedded/bpf/
	@cp configs/config.example.yaml pkg/embedded/configs/
	@cp packaging/systemd/conntrack.service pkg/embedded/systemd/ 2>/dev/null || true
	@echo "✓ Embedded resources prepared"

## build-conntrack-embedded: Сборка conntrack с embedded ресурсами
build-conntrack-embedded: ebpf-build prepare-embedded
	@mkdir -p $(BUILD_DIR)
	@echo "Building conntrack with embedded resources..."
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/conntrack ./cmd/conntrack
	@echo "✓ Built $(BUILD_DIR)/conntrack (single binary)"
	@ls -lh $(BUILD_DIR)/conntrack

## release-linux-amd64: Сборка релиза для linux/amd64
release-linux-amd64:
	@echo "Building release for linux/amd64..."
	GOOS=linux GOARCH=amd64 $(MAKE) build-conntrack-embedded
	@cp $(BUILD_DIR)/conntrack dist/conntrack-linux-amd64

## release: Создать все релиз артефакты
release: clean
	@echo "Creating release artifacts..."
	@mkdir -p dist
	$(MAKE) release-linux-amd64
	@cd dist && sha256sum conntrack-* > SHA256SUMS
	@echo "✓ Release artifacts created in dist/"
	@ls -lh dist/
```

---

## План реализации

### Этап 1: Подготовка (30 мин)
- [ ] Создать `pkg/embedded/` директорию
- [ ] Создать `packaging/systemd/` директорию
- [ ] Обновить `.gitignore` (игнорировать `pkg/embedded/bpf/*.o`)

### Этап 2: systemd unit (30 мин)
- [ ] Создать `packaging/systemd/conntrack.service`
- [ ] Протестировать на одном хосте вручную

### Этап 3: embedded пакет (1 час)
- [ ] Создать `pkg/embedded/embed.go`
- [ ] Добавить функции для всех ресурсов
- [ ] Добавить `HasEmbeddedEBPF()`
- [ ] Добавить `ExportEBPFToFile()`

### Этап 4: install/deinstall (2 часа)
- [ ] Создать `cmd/conntrack/install.go`
- [ ] Реализовать `runInstall()` с проверкой прав
- [ ] Реализовать `runDeinstall()` (без удаления config)
- [ ] Добавить логирование в stdout + syslog
- [ ] Добавить команду `show-config`

### Этап 5: export-ebpf-prog (30 мин)
- [ ] Добавить флаг `--export-ebpf-prog` в main.go
- [ ] Реализовать обработчик в run()
- [ ] Или создать отдельную команду `export-ebpf-prog`

### Этап 6: Интеграция (1 час)
- [ ] Обновить `cmd/conntrack/main.go`
- [ ] Обновить `internal/conntrack/tracker_linux.go`
- [ ] Приоритет: флаг → embedded → симуляция

### Этап 7: Makefile (30 мин)
- [ ] Добавить `prepare-embedded`
- [ ] Добавить `build-conntrack-embedded`
- [ ] Обновить `release`

### Этап 8: Тестирование (3 часа)
- [ ] Тест на 4 хостах (192.168.5.217/214/193/99)
- [ ] Тест `conntrack install`
- [ ] Тест `conntrack deinstall` (проверить что config сохранён)
- [ ] Тест `conntrack show-config`
- [ ] Тест `--export-ebpf-prog`
- [ ] Тест с `--ebpf-prog`
- [ ] Тест без embedded (build без embed)
- [ ] Проверка syslog логов

### Этап 9: CI/CD (1 час)
- [ ] Обновить `.github/workflows/release.yml`
- [ ] Добавить тест сборки

**Итого:** ~9 часов

---

## Команды приложения (сводка)

```bash
# Запуск
sudo conntrack --config /etc/conntrack/config.yaml

# Запуск с внешним .o
sudo conntrack --ebpf-prog /path/to/conntrack.bpf.o

# Установка
sudo conntrack install
sudo conntrack install --install-path /opt/bin

# Удаление (config сохраняется)
sudo conntrack deinstall

# Sample config
conntrack show-config
conntrack --show-config

# Экспорт .o
conntrack --export-ebpf-prog ./conntrack.bpf.o
```

---

## Размер бинарного файла

```
Базовый conntrack:     ~8 MB
eBPF программа:        ~21 KB
Sample config:         ~2 KB
systemd unit:          ~1 KB
Go runtime + deps:     ~7-10 MB
─────────────────────────────────
Итого:                 ~15-20 MB
```

---

## Вопросы

Все требования подтверждены. Готов приступить к реализации.
