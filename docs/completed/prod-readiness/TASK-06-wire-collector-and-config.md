# TASK-06 — Интеграция коллектора в main.go + переключатель источника

> ✅ **СТАТУС: ВЫПОЛНЕНО** (основная сессия). Реализовано:
> - `global.loss_source: ebpf|tracepipe` в конфиге (дефолт `ebpf`, пустое → `ebpf`), валидация; пример в `config.example.yaml`.
> - `main.go`: switch по `loss_source` через локальный интерфейс `lossCollector`; общий `SetReadyFunc`/`SetUp`/`setFatal`. `CheckAndWarnTracepoint` только для `tracepipe`. `source` в self-метрике = фактический источник (`ebpf`/`trace_pipe`).
> - **E2E на 4 ядрах (5.15/6.1/6.8/6.12):** обе ветки → `netmon_tcp_loss_total{src_role=…,dst_role=…}` растёт, `read==parsed`, `parse_errors=0`, `collector_up=1`, `/ready=200`.

**Метка исполнителя:** 🧠 strong
**Зависит от:** TASK-05
**Оценка:** ~0.5 дня

---

## Контекст (зачем)

Нужно подключить новый eBPF-коллектор потерь (TASK-05) в `cmd/netmon/main.go` и
дать конфигом выбор источника данных: `ebpf` (production, по умолчанию) или
`tracepipe` (старый, как fallback/для отладки).

Сейчас в `cmd/netmon/main.go:269` жёстко создаётся:
```go
collector := collector.NewTracePipeCollector(cfg.Global.TracePipePath, exporter, logger)
go func() { if err := collector.Run(ctx); err != nil { logger.Error(...) } }()
```

## Что сделать

### 1. Конфиг: добавить выбор источника

В `internal/config/config.go`, в `GlobalConfig` (строки ~27-32) добавь поле:
```go
type GlobalConfig struct {
    TTLHours      int    `yaml:"ttl_hours"`
    MetricsPort   int    `yaml:"metrics_port"`
    TracePipePath string `yaml:"trace_pipe_path"`
    LossSource    string `yaml:"loss_source"` // "ebpf" (default) | "tracepipe"
}
```
- В `DefaultConfig()` задай `LossSource: "ebpf"`.
- В `Validate()` добавь проверку допустимых значений:
  ```go
  validLossSources := map[string]bool{"ebpf": true, "tracepipe": true}
  if c.Global.LossSource == "" { c.Global.LossSource = "ebpf" } // на случай старых конфигов
  if !validLossSources[c.Global.LossSource] {
      return fmt.Errorf("invalid loss_source: %s (valid: ebpf, tracepipe)", c.Global.LossSource)
  }
  ```
- Обнови примеры конфигов: `configs/config.example.yaml`, `configs/netmon.yaml.example`
  — добавь закомментированный/дефолтный `loss_source: ebpf` с пояснением.

### 2. main.go: выбор коллектора

Замени блок создания коллектора (около строки 268-274) на логику выбора. Коллектор
должен реализовывать общий интерфейс запуска. Введи локальный интерфейс:
```go
type lossCollector interface {
    Run(ctx context.Context) error
}
```
и выбирай реализацию:
```go
var lc lossCollector
switch cfg.Global.LossSource {
case "tracepipe":
    logger.Warn("Using legacy trace_pipe loss source (not recommended for production)")
    lc = collector.NewTracePipeCollector(cfg.Global.TracePipePath, exporter, logger)
default: // "ebpf"
    c, err := losscollector.NewEBPFLossCollector(exporter, logger, losscollector.Options{})
    if err != nil {
        logger.Error("Failed to init eBPF loss collector", zap.Error(err))
        // см. TASK-09: это должно влиять на readiness / приводить к фатальному завершению
        cancel()
    } else {
        lc = c
    }
}
if lc != nil {
    go func() {
        if err := lc.Run(ctx); err != nil && err != context.Canceled {
            logger.Error("Loss collector error", zap.Error(err))
            // см. TASK-09
        }
    }()
}
```
> Не забудь импорт `internal/losscollector`.
> Сохрани возможность указать явный путь к `.o` через флаг/конфиг, если это заложено в TASK-05 (для отладки).

### 3. Логи и стартовое сообщение

В сообщении «Network Monitor started» (около строки 408) добавь поле
`zap.String("loss_source", cfg.Global.LossSource)`.

### 4. Взаимодействие с conntrack

Conntrack (`cfg.Connections.Enabled`) остаётся как есть. Но обрати внимание: если
включён `loss_source: ebpf`, старый `CheckAndWarnTracepoint` и логика
`--enable-tracing` (относящиеся к текстовому trace_pipe) больше не обязательны для
пути потерь. НЕ удаляй их, но:
- при `loss_source == "ebpf"` НЕ делай warning про выключенный текстовый трейспоинт
  (он не нужен для eBPF-пути). Оберни `collector.CheckAndWarnTracepoint(...)` в
  условие `if cfg.Global.LossSource == "tracepipe"`.

## Критерии приёмки (Definition of Done)

1. `loss_source: ebpf` (по умолчанию) запускает eBPF-коллектор из TASK-05.
2. `loss_source: tracepipe` запускает старый `TracePipeCollector` (с предупреждением в логе).
3. Невалидное значение `loss_source` → ошибка на этапе `config.Validate()`.
4. Пустой `loss_source` в старых конфигах трактуется как `ebpf` (обратная совместимость).
5. `CheckAndWarnTracepoint` вызывается только для `tracepipe`.
6. `go build ./...`, `go vet ./...`, `go test ./...` — зелёные.

## Как проверить
```bash
go build ./...
go test ./internal/config/...     # добавь тест на валидацию loss_source
# Linux E2E: запустить с ebpf и с tracepipe, убедиться что метрика растёт (ebpf) /
# работает старый путь (tracepipe).
```

## Риски
- Не сломать существующее поведение при отсутствии поля в конфиге (миграция).
- Согласуй обработку фатальной ошибки инициализации eBPF с TASK-09.
