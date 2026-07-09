# TASK-05 — Go-коллектор потерь поверх eBPF ring buffer

> ✅ **СТАТУС: ВЫПОЛНЕНО** (основная сессия). Реализовано:
> - `internal/losscollector/ebpf_linux.go` (build tag linux) + `ebpf_other.go` (stub для сборки на macOS).
> - Грузит embedded `.o` (`embedded.GetTCPLossEBPFProgram`), аттач `link.Tracepoint("tcp","tcp_retransmit_skb")`, ringbuf-reader на `loss_events`.
> - Корректный выход из цикла: reader закрывается по `ctx.Done`, `errors.Is(ErrClosed)` → return, backoff на прочих ошибках (нет busy-loop).
> - `validateBpfLossEvent()` (размер 48 / офсеты); парсинг → `RecordRetransmit`; IPv4-mapped → dotted-quad.
> - Self-метрики через интерфейс `Metrics` (duck-typing на `*collector.CollectorMetrics`, без импорта collector) + atomic-геттеры для тестов.
> - Unit-тесты (linux): `validateBpfLossEvent`, парсинг валидного/короткого/не-IPv4 события — зелёные на debian13.

**Метка исполнителя:** 🧠 strong 🐧 linux-host
**Зависит от:** TASK-04
**Оценка:** 1–2 дня

---

## Контекст (зачем)

Нужен Go-коллектор, который загружает eBPF-программу из TASK-04, читает
`loss_events` ring buffer, парсит структурированные события и вызывает
`exporter.RecordRetransmit(srcIP, dstIP)`. Он заменяет текстовый парсер
`internal/collector/trace_pipe.go` как **production-источник** данных.

**Референс:** `internal/conntrack/tracker_linux.go` — там уже есть полный пример:
загрузка `.o` (`loadEmbeddedEBPF`/`loadEBPFFromFile`), `ringbuf.NewReader`, цикл
чтения `readEvents`, парсинг бинарного события (`parseConnectionEvent`), валидация
размера структуры (`validateBpfConnectionEvent`). Копируй паттерны оттуда.

## Интерфейс, который надо соблюсти

Экспортёр (`internal/metrics/exporter.go`) уже реализует нужный контракт:
```go
type RetransmitExporter interface {   // объявлен в internal/collector/trace_pipe.go
    RecordRetransmit(srcIP, dstIP string)
}
```
Новый коллектор должен принимать такой же `RetransmitExporter`, чтобы быть
drop-in заменой текущего `TracePipeCollector`.

## Что сделать

### 1. Новый пакет/файлы

Создай (с build-тегами `linux`, как в conntrack):
- `internal/losscollector/ebpf_linux.go` — реализация на eBPF (тег `//go:build linux`).
- `internal/losscollector/ebpf_other.go` — заглушка для не-Linux (тег `//go:build !linux`),
  чтобы `go build ./...` собирался на macOS. Заглушка возвращает ошибку
  «eBPF loss collector is Linux-only» из `Run`.

> Имя пакета `losscollector` — чтобы не путать с существующим `collector`
> (trace_pipe). Если предпочитаешь положить рядом в `internal/collector` с суффиксом
> файлов — допустимо, но не ломай существующий `TracePipeCollector`.

### 2. Embed нового .o

В `pkg/embedded/embed.go` добавь:
```go
//go:embed bpf/tcploss.bpf.o
var tcpLossEBPFData []byte

func GetTCPLossEBPFProgram() ([]byte, error) {
    if len(tcpLossEBPFData) == 0 {
        return nil, fmt.Errorf("embedded tcploss eBPF program not available")
    }
    return tcpLossEBPFData, nil
}

func HasTCPLossEBPF() bool { return len(tcpLossEBPFData) > 0 }
```
(файл `pkg/embedded/bpf/tcploss.bpf.o` кладётся в TASK-04).

### 3. Структура события в Go (побайтово совпадает с TASK-04)

```go
// Должна ТОЧНО соответствовать struct tcploss_event из bpf/tcploss.bpf.c (48 байт)
type bpfLossEvent struct {
    TimestampNs uint64   // offset 0
    SrcIP       [16]byte // offset 8
    DstIP       [16]byte // offset 24
    SrcPort     uint16   // offset 40
    DstPort     uint16   // offset 42
    Family      uint8    // offset 44
    _           [3]byte  // offset 45..47
}
```
Добавь функцию `validateBpfLossEvent()` по образцу `validateBpfConnectionEvent`
(проверка `unsafe.Sizeof == 48` и офсета `SrcIP`). Вызывай её при создании коллектора.

### 4. Загрузка и чтение

- `rlimit.RemoveMemlock()` (как в conntrack).
- Загрузи spec из embedded `.o` (или из явного пути-флага — по образцу приоритетов
  в `loadEBPF` conntrack: явный путь → embedded → ошибка). Для loss-коллектора
  **симуляции не делай** — если eBPF недоступен, возвращай ошибку из `Run`.
- Приаттачь `link.Tracepoint("tcp", "tcp_retransmit_skb", prog, nil)`.
- Открой `ringbuf.NewReader` на map `loss_events`.
- В цикле читай записи, парси в `bpfLossEvent`, конвертируй IP через хелпер типа
  `IPFromBytes` (в conntrack есть `IPFromBytes` для `[16]byte` IPv4-mapped — переиспользуй
  логику: срез `[12:16]` для IPv4). Вызывай `exporter.RecordRetransmit(src, dst)`.

### 5. ВАЖНО: корректный выход из цикла чтения (не busy-loop)

В conntrack есть баг — при ошибке `rd.Read()` делается `continue`, что при закрытом
reader'е даёт CPU-спин. НЕ повторяй его. Правильный паттерн:
```go
import "errors"
import "github.com/cilium/ebpf/ringbuf"

for {
    record, err := rd.Read()
    if err != nil {
        if errors.Is(err, ringbuf.ErrClosed) {
            c.logger.Info("ringbuf closed, stopping loss collector")
            return nil
        }
        c.logger.Warn("reading loss ringbuf", zap.Error(err))
        // backoff, чтобы не крутить CPU при устойчивой ошибке
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-time.After(100 * time.Millisecond):
        }
        continue
    }
    // ... parse & RecordRetransmit
}
```
Для отмены по контексту: закрывай `rd` в горутине при `<-ctx.Done()` — тогда
`rd.Read()` вернёт `ringbuf.ErrClosed` и цикл выйдет. (см. как это делают в
примерах cilium/ebpf; в conntrack используется проверка `ctx` в начале — можно
совместить оба подхода: closer-горутина + `errors.Is(err, ringbuf.ErrClosed)`).

### 6. Публичный API коллектора

Сделай сигнатуру, симметричную `TracePipeCollector`:
```go
type EBPFLossCollector struct { ... }
func NewEBPFLossCollector(exporter RetransmitExporter, logger *zap.Logger, opts Options) (*EBPFLossCollector, error)
func (c *EBPFLossCollector) Run(ctx context.Context) error
func (c *EBPFLossCollector) Close() error
```
`Options` может включать: явный путь к `.o` (для отладки), размер буфера.
Также предусмотри геттеры для метрик самонаблюдения (используются в TASK-08):
`EventsRead() uint64`, `EventsParsed() uint64`, `ParseErrors() uint64`.

## Критерии приёмки (Definition of Done)

1. `go build ./...` собирается и на Linux, и на macOS (за счёт `_other.go` заглушки).
2. На Linux-хосте коллектор грузит eBPF, цепляет трейспоинт, читает события и
   вызывает `RecordRetransmit`.
3. `validateBpfLossEvent()` совпадает с C-структурой (тест на размер/офсеты).
4. Цикл чтения корректно завершается по `ctx.Done()` без busy-loop; обрабатывает `ringbuf.ErrClosed`.
5. Есть счётчики `EventsRead/EventsParsed/ParseErrors`.
6. Unit-тест на парсинг: подать сырые байты валидного события → получить правильные IP/порты.

## Как проверить
```bash
GOOS=darwin go build ./...     # заглушка компилируется
go build ./...                 # (на Linux) реальная реализация
go test ./internal/losscollector/...
# E2E на Linux: запустить netmon (после TASK-06), навесить tc netem loss,
# убедиться что netmon_tcp_loss_total растёт по +1 на ретрансмит.
sudo tc qdisc add dev lo root netem loss 20%   # (осторожно, потом удалить: tc qdisc del dev lo root)
```

## Ограничения / риски

- IPv4-only (см. TASK-04).
- Байт-порядок портов/IP — держи единое соглашение с TASK-04.
- Проверь, что не осталось зависимости прода от `TracePipeCollector` после TASK-06.
