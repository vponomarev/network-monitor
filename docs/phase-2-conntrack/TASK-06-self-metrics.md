# TASK-06 — (C-4) Self-метрики conntrack + экспонирование дропов

**Метка исполнителя:** 🧠 strong (или 👷 qwen-ok с опорой на netmon)
**Зависит от:** —
**Оценка:** ~0.5–1 день

---

## Контекст (проблема)

- `droppedEvents` считается атомарно (`tracker_linux.go:610-617`), но по факту
  виден в `/metrics` только когда conntrack встроен в netmon (метрики регистрируются
  в default-registry, а свой HTTP standalone conntrack не поднимает — см. TASK-07).
- **Нет** счётчиков «прочитано событий из ringbuf» / «успешно распарсено» /
  «ошибок парсинга» — то есть нельзя отличить «трафика нет» от «ридер молча умер».

netmon уже имеет такой набор в `internal/collector/metrics.go`
(`netmon_loss_collector_up`, `..._events_read_total`, `..._events_parsed_total`,
`..._parse_errors_total`, `netmon_loss_source_info`) — используй как образец
именования и симметрии `up` 1/0.

## Что сделать

1. В `internal/conntrack/metrics.go` добавить (по образцу netmon):
   - `conntrack_collector_up` (Gauge, 1 при работающем ридере, 0 при остановке/фейле);
   - `conntrack_events_read_total` (Counter) — инкремент на каждую запись из ringbuf;
   - `conntrack_events_parsed_total` (Counter) — успешный `parseConnectionEvent`;
   - `conntrack_parse_errors_total` (Counter) — ошибка/короткое событие;
   - `conntrack_dropped_events_total` — **убедиться, что уже существующий**
     `droppedEvents` экспонируется как Counter (C-4), а не только считается.
   - опц. `conntrack_source_info{source="ebpf|simulation"}` — симметрично netmon.
2. Расставить инкременты в hot-path (`readEvents`/`onConnectionEvent`/парсинг),
   аналогично тому, где netmon инкрементит read/parsed/errors.
3. `up` выставлять в 1 после успешного attach+запуска ридера и в 0 при
   остановке/фатальной ошибке (согласовать с TASK-08).
4. Проверить регистрацию: метрики должны реально попадать в отдаваемый
   `/metrics` (в связке с TASK-07 для standalone; и продолжать работать во
   встроенном режиме netmon).

## Критерии приёмки (DoD)
1. В `/metrics` присутствуют `conntrack_collector_up`,
   `conntrack_events_read_total`, `conntrack_events_parsed_total`,
   `conntrack_parse_errors_total`, `conntrack_dropped_events_total`.
2. `up` симметрично 1/0 при старте/остановке.
3. `read` растёт при событиях; `parsed` ≈ `read` при корректном формате;
   `parse_errors` = 0 в норме.
4. Не сломаны существующие метрики `conntrack_*` (не менять их имена).
5. `go build ./... && go test ./... && gofmt -l .` — чисто.

## Как проверить
```bash
# встроенный в netmon режим:
curl -s localhost:9876/metrics | grep -E '^conntrack_(collector_up|events_(read|parsed)_total|parse_errors_total|dropped_events_total)'
# создать исходящие соединения и убедиться, что read/parsed растут синхронно
```

## Риски
- Не регистрировать метрики дважды (default registry) — при встраивании в netmon
  избегать паники `duplicate registration`. Согласовать регистрацию с TASK-07
  (передавать `*prometheus.Registry` вместо MustRegister в default, если удобно).
- Счётчики — это `Counter` (монотонные), а не `Gauge`; `up` — `Gauge`.
