# TASK-09 — Разметка соединений conntrack по ролям/локациям + кардинальность

**Метка исполнителя:** 🧠 strong
**Зависит от:** TASK-06 (метрики)
**Оценка:** ~1 день

---

## Контекст (зачем)

netmon размечает потери TCP по ролям/локациям src/dst через `internal/metadata`
(longest-prefix match по маскам из `roles.yaml`/`locations.yaml`), а кардинальность
метрик контролируется уровнями `ip|network|role` + `max_series` (Фаза 1, TASK-10).

conntrack сейчас так не умеет: соединения не размечаются по ролям, и нет контроля
кардинальности его Prometheus-метрик. Для единообразной наблюдаемости стоит дать
conntrack ту же разметку.

## Что сделать

1. Переиспользовать `internal/metadata` (RoleMatcher/LocationMatcher) в conntrack:
   на событие соединения матчить `SourceIP`/`DestIP` → роль/локация.
2. Добавить в метрики conntrack метки `src_role/dst_role`, `src_location/dst_location`
   (и, при необходимости, `src_vrf/dst_vrf`) — по образцу набора меток netmon
   (`internal/metrics/exporter.go`, `labelNamesForLevel`).
3. Ввести контроль кардинальности (уровни `ip|network|role`, `max_series`) по
   образцу netmon TASK-10: агрегировать до роли по умолчанию, ограничивать число
   активных серий, экспонировать `conntrack_active_series` /
   `conntrack_series_dropped_total`.
4. Конфиг conntrack расширить секцией `metadata` (пути к roles/locations) и
   `metrics.cardinality`, аналогично netmon.

## Критерии приёмки (DoD)
1. Метрики conntrack содержат метки ролей/локаций; matcher использует
   longest-prefix (переиспользован `internal/metadata`, без дубля логики).
2. Уровень кардинальности по умолчанию — `role`; `max_series` ограничивает рост;
   при превышении растёт `conntrack_series_dropped_total`.
3. Не сломаны имена существующих метрик (только добавление меток/серий —
   согласовать, т.к. добавление меток тоже меняет идентичность серий; при
   необходимости завести новые метрики, а старые сохранить).
4. `go build ./... && go test ./... && gofmt -l .` — чисто.

## Как проверить
```bash
# подготовить roles.yaml/locations.yaml, создать соединения к размеченным сетям:
curl -s localhost:9877/metrics | grep -E '^conntrack_.*\{.*(src_role|dst_role)='
```

## Риски
- Разметка на hot-path: matcher должен быть быстрым (longest-prefix уже O(маски));
  при высоком churn — не блокировать (кэшировать, если понадобится).
- Изменение набора меток ломает старые дашборды/запросы — согласовать с владельцем,
  задокументировать в TASK-12.
- Это самая «продуктовая» задача фазы; при нехватке времени может быть отложена
  после стабильности/переносимости.
