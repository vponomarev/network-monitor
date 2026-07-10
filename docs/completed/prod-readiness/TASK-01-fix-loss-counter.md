# TASK-01 — Исправить неверный подсчёт метрики потерь + утечку серий

**Метка исполнителя:** 👷 qwen-ok
**Зависит от:** нет
**Оценка:** ~1 час

---

## Контекст (проблема)

`netmon_tcp_loss_total` — это ГЛАВНАЯ метрика приложения. Сейчас она считается
неправильно: значение раздувается по «треугольным числам».

Файл: `internal/metrics/exporter.go`

Текущий код (упрощённо):
```go
// RecordRetransmit вызывается на КАЖДЫЙ ретрансмит
func (e *Exporter) RecordRetransmit(srcIP, dstIP string) {
    key := pairKey{src: srcIP, dst: dstIP}
    e.mu.Lock()
    defer e.mu.Unlock()
    if data, ok := e.events[key]; ok {
        data.count++            // накопительный счётчик
        data.lastSeen = time.Now()
    } else {
        e.events[key] = &pairData{count: 1, lastSeen: time.Now()}
    }
    e.updateMetric(key)         // <-- проблема здесь
}

func (e *Exporter) updateMetric(key pairKey) {
    data := e.events[key]
    ...
    e.counter.WithLabelValues(...).Add(float64(data.count)) // <-- BUG: прибавляет ВСЮ сумму
}
```

**Почему баг:** `updateMetric` делает `counter.Add(data.count)` при каждом событии,
а `data.count` — это накопленное число. Прометеевский `CounterVec` уже сам хранит
сумму. В итоге:

| Событие № | `data.count` | `Add()` прибавит | Итог метрики |
|-----------|--------------|------------------|--------------|
| 1 | 1 | +1 | 1 |
| 2 | 2 | +2 | 3 |
| 3 | 3 | +3 | 6 |
| N | N | +N | N·(N+1)/2 |

Должно быть просто N (по одному на событие).

**Вторая проблема (утечка серий):** метод `cleanupOld()` (вызывается из `Collect`)
удаляет ключи только из `e.events`, но НЕ удаляет соответствующие серии из
`e.counter`. Значит серии Prometheus живут вечно → рост кардинальности и памяти.
TTL по факту не работает для самих метрик.

---

## Что сделать

### Шаг 1. Считать по +1 на событие

В `internal/metrics/exporter.go`:

1. В `RecordRetransmit` заменить вызов `e.updateMetric(key)` на прямой инкремент
   счётчика на **единицу**:
   ```go
   e.counter.WithLabelValues(
       key.src, key.dst,
       srcLocation, dstLocation,
       srcRole, dstRole,
       srcNetwork, dstNetwork,
       srcVrf, dstVrf,
   ).Inc()
   ```
   Значения лейблов получай ровно так же, как сейчас в `updateMetric`
   (`e.locationMatcher.GetLocation(...)`, `getNetwork(...)`, `e.locationMatcher.GetVrf(...)` и т.д.).

2. Внутренний счётчик `pairData.count` продолжай инкрементить — он нужен для TTL и
   для API «top loss». То есть `data.count++` оставить, а из процесса обновления
   Prometheus-метрики убрать зависимость от `data.count`.

3. Метод `updateMetric` (который делает `.Add(float64(data.count))`) — переписать
   так, чтобы он больше НЕ использовался для инкремента на событие. Проще всего:
   удалить `updateMetric` и перенести вычисление лейблов прямо в `RecordRetransmit`
   (или в приватный хелпер `labelsFor(key) []string`, чтобы не дублировать код с
   `updateMetricLocked`, который используется в `SetMatchers`).

> ⚠️ ВНИМАНИЕ на `SetMatchers` / `updateMetricLocked`: при перезагрузке matcher'ов
> код делает `counter.Reset()` и затем пересоздаёт метрики со значением
> `Add(float64(data.count))`. Это КОРРЕКТНО и должно остаться — там `data.count`
> это полное накопленное значение, а счётчик только что сброшен. НЕ трогай эту
> ветку логики, кроме случая, если выносишь общий хелпер `labelsFor`.

### Шаг 2. Чистить серии Prometheus в cleanupOld

В `cleanupOld()` при удалении устаревшего ключа из `e.events` — также удалять
серию из счётчика:
```go
for key, data := range e.events {
    if now.Sub(data.lastSeen) > e.ttl {
        // вычислить те же лейблы, что использовались при создании серии
        labels := e.labelsFor(key) // [src, dst, srcLoc, dstLoc, srcRole, dstRole, srcNet, dstNet, srcVrf, dstVrf]
        e.counter.DeleteLabelValues(labels...)
        delete(e.events, key)
    }
}
```
> `prometheus.CounterVec` имеет метод `DeleteLabelValues(lvs ...string) bool`.
> Лейблы должны совпадать с теми, что были при создании (тот же порядок и значения).
> Поскольку location/role для IP могут измениться между созданием и удалением
> (после reload), допустимо, что `DeleteLabelValues` вернёт `false` — это не ошибка,
> просто залогируй на Debug. Идеально — хранить в `pairData` снимок использованных
> лейблов; если делаешь это, добавь поле `labels []string` в `pairData` и заполняй
> его в `RecordRetransmit`. Это предпочтительный вариант, реализуй его.

---

## Критерии приёмки (Definition of Done)

1. После N вызовов `RecordRetransmit("a","b")` подряд метрика с этой парой равна ровно `N`.
2. `updateMetric` больше не прибавляет накопленную сумму на каждое событие
   (либо удалён, либо не вызывается из `RecordRetransmit`).
3. `SetMatchers` (SIGHUP reload) по-прежнему сохраняет накопленные значения счётчиков.
4. `cleanupOld` удаляет соответствующую серию из `CounterVec`, а не только из `events`.
5. Добавлен/обновлён unit-тест (см. ниже).

## Как проверить

Добавь тест в `internal/metrics/exporter_test.go`:
```go
func TestRecordRetransmit_CountsOnePerEvent(t *testing.T) {
    // используй NewExporterWithRegistry с prometheus.NewRegistry()
    reg := prometheus.NewRegistry()
    exp := NewExporterWithRegistry("test_loss_total",
        metadata.NewEmptyLocationMatcher(zap.NewNop()),
        metadata.NewEmptyRoleMatcher(zap.NewNop()),
        zap.NewNop(), reg)

    for i := 0; i < 5; i++ {
        exp.RecordRetransmit("10.0.0.1", "10.0.0.2")
    }
    // Собрать метрику и проверить, что значение == 5, а не 15.
    // Используй testutil.ToFloat64 из github.com/prometheus/client_golang/prometheus/testutil
    // на конкретной серии, либо reg.Gather() и найти нужную пару.
}
```
Проверь также существующие тесты `GetEventCount`.

```bash
go test ./internal/metrics/... -run Retransmit -v
go test ./...
```

## Риски

- Не сломать `SetMatchers` — там `Add` корректен после `Reset`. Прогони весь пакет.
- `DeleteLabelValues` требует точного совпадения лейблов — используй сохранённый снимок лейблов из `pairData`.
