# TASK-07 — Реальные /health и /ready

> ✅ **СТАТУС: ВЫПОЛНЕНО** (реализовано в основной сессии, развязано от TASK-06).
> Готовность привязана к текущему коллектору (trace_pipe), а не к eBPF-цепочке, —
> поэтому зависимость от TASK-06 снята. Когда появится eBPF-коллектор (TASK-05/06),
> он просто вызывает тот же `health.State.SetCollectorReady(...)`.
>
> Что сделано:
> - `internal/health/health.go` — `State` (atomic) + `LivenessHandler`/`ReadinessHandler`.
> - `internal/collector/trace_pipe.go` — `SetReadyFunc` + `signalReady` (once) после успешного открытия trace_pipe.
> - `cmd/netmon/main.go` — проводка: `/health`=liveness (всегда 200), `/ready`=503 пока коллектор не стартовал, 200 после; сброс в 503 при остановке коллектора.
> - `internal/metrics/server.go` — `NewServer` помечен Deprecated (мёртвый код с always-200 stub).
> - Тесты: `internal/health/health_test.go` (+ `-race`), `internal/collector/trace_pipe_ready_test.go` (ready-once).
> - Проверено: `go build ./...` (darwin+linux), `go vet`, `gofmt`, `go test -race ./...` — зелёные.

**Метка исполнителя:** 👷 qwen-ok
**Зависит от:** ~~TASK-06~~ снято — реализовано поверх текущего коллектора.
**Оценка:** ~2-3 часа

---

## Контекст (проблема)

Сейчас `/health` и `/ready` ВСЕГДА возвращают `200 OK`:
- `cmd/netmon/main.go:357-365`
- `internal/metrics/server.go:32-41`

Это опасно в проде (systemd/k8s): если коллектор потерь не запустился (eBPF не
загрузился, трейспоинт не приаттачился, источник недоступен) — процесс жив и
рапортует «здоров», хотя данных нет. Оркестратор не перезапустит и не выведет из
ротации.

## Что сделать

Ввести общий статус готовности, который отражает фактическое состояние коллектора.

### 1. Простой потокобезопасный «readiness state»

Создай маленький хелпер (например, `internal/health/health.go`):
```go
package health

import "sync/atomic"

// State хранит готовность компонентов приложения.
type State struct {
    collectorReady atomic.Bool
}

func NewState() *State { return &State{} }

func (s *State) SetCollectorReady(ready bool) { s.collectorReady.Store(ready) }
func (s *State) Ready() bool                  { return s.collectorReady.Load() }
```
> Если появятся другие критичные компоненты — расширишь. Пока достаточно одного флага «коллектор потерь готов».

### 2. Проставлять готовность из коллектора

В `cmd/netmon/main.go`:
- создай `hs := health.NewState()` до запуска коллектора;
- коллектор потерь после успешного старта (eBPF загружен, трейспоинт приаттачен,
  ringbuf-reader открыт) должен помечать готовность. Два варианта — выбери проще:
  - **A (рекомендуется):** коллектор принимает callback `onReady func()` или
    `*health.State`, и сам вызывает `SetCollectorReady(true)` после успешного attach,
    и `SetCollectorReady(false)` при выходе/фатальной ошибке.
  - **B:** в `main.go` после `NewEBPFLossCollector(...)` без ошибки → `hs.SetCollectorReady(true)`.
    (Менее точно: не гарантирует, что ringbuf реально читается, но приемлемо как минимум.)

### 3. Хендлеры

- `/health` — **liveness**: процесс жив и HTTP отвечает. Оставь `200 OK` всегда
  (это корректно для liveness). Добавь в тело мелкий JSON, например `{"status":"ok"}`.
- `/ready` — **readiness**: возвращай `200` только если `hs.Ready() == true`, иначе
  `503 Service Unavailable` с телом `{"status":"not ready","reason":"loss collector not started"}`.

Пример `/ready`:
```go
mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
    if !hs.Ready() {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusServiceUnavailable)
        w.Write([]byte(`{"status":"not ready","reason":"loss collector not started"}`))
        return
    }
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    w.Write([]byte(`{"status":"ready"}`))
})
```
Обнови обе копии хендлеров: и в `cmd/netmon/main.go`, и в
`internal/metrics/server.go` (проверь, какая реально используется netmon — в main.go
свой `mux`; `server.go` может быть неиспользуемым дубликатом. Если `server.go` не
используется netmon — приведи его в соответствие или пометь как устаревший, но не ломай сборку).

### 4. Особый случай tracepipe

При `loss_source: tracepipe` готовность выставляй после успешного `os.Open` trace_pipe
внутри старого коллектора (или в main.go — упрощённо `true`, т.к. старый путь не для прода).

## Критерии приёмки (Definition of Done)

1. Пока коллектор потерь не стартовал успешно — `/ready` возвращает `503`.
2. После успешного старта — `/ready` возвращает `200`.
3. `/health` всегда `200` (liveness).
4. Нет гонок данных (используется `atomic` или мьютекс). Проверить `go test -race`.
5. Тест на хендлер `/ready` в обоих состояниях (`httptest`).

## Как проверить
```bash
go test -race ./internal/health/... ./cmd/netmon/... 2>/dev/null
go build ./...
# Linux руками: остановить/сломать eBPF → /ready даёт 503; здоровый старт → 200.
curl -i http://localhost:9876/ready
```

## Риски
- Убедись, что готовность выставляется в false при фатальной ошибке коллектора (связка с TASK-09).
