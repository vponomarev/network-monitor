# TASK-08 — Метрики самонаблюдения коллектора потерь

**Метка исполнителя:** 👷 qwen-ok
**Зависит от:** TASK-05 (для eBPF-счётчиков) — но базовые метрики можно добавить и раньше
**Оценка:** ~2-3 часа

---

## Контекст (проблема)

Сейчас у пути сбора потерь нет наблюдаемости: если коллектор ничего не собирает —
понять это по метрикам нельзя. Нет счётчиков прочитанных/распознанных/дропнутых
событий, нет индикатора «источник жив».

Для прода нужны метрики, по которым строятся алерты «сбор данных сломался».

## Что сделать

Добавь набор Prometheus-метрик, публикуемых в тот же реестр
(`prometheus.DefaultRegisterer`, как в `internal/metrics/exporter.go`).

### Рекомендуемые метрики

| Метрика | Тип | Описание |
|---------|-----|----------|
| `netmon_loss_collector_up` | Gauge | 1 если коллектор активен и источник приаттачен, иначе 0 |
| `netmon_loss_events_read_total` | Counter | Сколько событий прочитано из источника (ringbuf/trace_pipe) |
| `netmon_loss_events_parsed_total` | Counter | Сколько успешно распарсено и учтено |
| `netmon_loss_events_parse_errors_total` | Counter | Ошибки парсинга/битые события |
| `netmon_loss_source_info` | Gauge (=1) с лейблом `source="ebpf"\|"tracepipe"` | Инфо-метрика о выбранном источнике |

> Для eBPF ringbuf «read» может ≈ «parsed». Для trace_pipe `read` = прочитанных
> строк, `parsed` = совпавших с паттерном retransmit. Семантику опиши в `Help`.

### Реализация

1. Создай коллектор метрик, например `internal/losscollector/metrics.go` (или
   расширь существующий exporter). Придерживайся стиля `internal/conntrack/metrics.go`
   (`MetricsCollector`), где уже есть регистрация counter/gauge — используй как образец.
2. Метрики инкрементируй в местах:
   - `up` = 1 после успешного attach/open; `up` = 0 при выходе/ошибке.
   - `events_read` — на каждую прочитанную запись.
   - `events_parsed` — при успешном `RecordRetransmit`.
   - `parse_errors` — когда событие не распарсилось / слишком короткое / битое.
3. Если TASK-05 уже добавил геттеры `EventsRead()/EventsParsed()/ParseErrors()`,
   можно обновлять gauge/counter периодически (тикер, как `updateMetrics` в
   `internal/conntrack/tracker_linux.go:716`), либо инкрементировать напрямую в
   цикле чтения. Выбери прямой инкремент — точнее.

### Регистрация без паники при повторной регистрации

Используй `NewExporterWithRegistry`-подобный подход: передавай `prometheus.Registerer`
и регистрируй через `MustRegister`. Для тестов принимай кастомный реестр
(`prometheus.NewRegistry()`), чтобы не конфликтовать с `DefaultRegisterer`.

## Критерии приёмки (Definition of Done)

1. Метрики `netmon_loss_collector_up`, `..._events_read_total`, `..._events_parsed_total`,
   `..._events_parse_errors_total`, `netmon_loss_source_info` появляются в `/metrics`.
2. `up` переключается в 0 при остановке/ошибке коллектора и в 1 при успешном старте.
3. Счётчики монотонно растут при поступлении событий.
4. Unit-тест: сымитировать N прочитанных событий → проверить значения счётчиков через кастомный реестр.
5. `go test ./...` зелёный, без паник двойной регистрации.

## Как проверить
```bash
go test ./internal/losscollector/...
curl -s http://localhost:9876/metrics | grep netmon_loss_
```
Пример алерта (для документации, не обязателен к реализации здесь):
```promql
# сбор данных сломан
netmon_loss_collector_up == 0
# читаем, но ничего не парсим (сломался формат)
rate(netmon_loss_events_read_total[5m]) > 0 and rate(netmon_loss_events_parsed_total[5m]) == 0
```

## Риски
- Не дублируй регистрацию метрик при повторном создании коллектора (тесты!).
