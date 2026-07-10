# TASK-12 — Прод-документация conntrack (EN/RU)

**Метка исполнителя:** 👷 qwen-ok
**Зависит от:** TASK-06, TASK-07, TASK-08 (чтобы документировать финальные метрики/эндпоинты/поведение)
**Оценка:** ~0.5 дня

---

## Контекст (зачем)

У netmon есть выверенная прод-документация `docs/PRODUCTION_en.md` /
`docs/PRODUCTION_ru.md` (требования к ядру/BTF, capabilities, systemd, метрики,
алерты). Для conntrack такого единого прод-документа нет (есть разрозненные
`docs/CONNTRACK*.md`, частично устаревшие). После доведения conntrack до
прод-готовности нужен аналогичный документ.

## Что сделать

Создать `docs/CONNTRACK_PRODUCTION_en.md` и `..._ru.md` (по структуре
`PRODUCTION_*`), выверенные по реальному коду после TASK-01…TASK-08:
1. **Требования:** ядро (после фикса — 5.15+…6.17; BTF), capabilities
   (по аналогии с netmon: `CAP_SYS_ADMIN CAP_NET_RAW` как рекомендованный набор —
   **проверить на практике**, что нужно conntrack для его хуков; kprobe/tracepoint
   могут требовать иных прав), файловая система.
2. **Конфигурация:** флаги/конфиг `cmd/conntrack` (`--ebpf-prog`, `--config`,
   syslog-*, track-*, порт HTTP, auth-токен, `--simulate` из TASK-08),
   разметка ролей (TASK-09), кардинальность.
3. **systemd:** актуализировать `packaging/systemd/conntrack.service` (по образцу
   выверенного `packaging/netmon.service`); capabilities, hardening, Restart.
4. **Метрики и алерты:** `conntrack_collector_up`, `..._events_read/parsed_total`,
   `..._parse_errors_total`, `..._dropped_events_total`, серии кардинальности;
   PromQL-алерты (по образцу netmon: CollectorDown, ParseErrors, ReadNotParsed,
   SeriesDropped).
5. **Эндпоинты:** `/health`, `/ready`, `/metrics`, `/api/v1/conntrack/*`.
6. **Ограничения:** IPv4/IPv6 (в зависимости от статуса TASK-10), отличия от netmon.
7. Добавить ссылки в корневой `README.md` (раздел Production, рядом с netmon).

## Критерии приёмки (DoD)
1. EN и RU документы существуют, технически соответствуют коду после Фазы 2.
2. Имена метрик/эндпоинтов/флагов совпадают с реальными.
3. `packaging/systemd/conntrack.service` выверен и соответствует доке
   (проверить установкой, если есть Linux-хост).
4. Ссылки добавлены в корневой README; битых ссылок нет.

## Как проверить
- Сверить каждую упомянутую метрику/флаг/эндпоинт с кодом и `curl`-проверкой
  (как в netmon PRODUCTION troubleshooting).

## Риски
- Не документировать несуществующее (частая ошибка старых `CONNTRACK*.md`) —
  сверять с кодом, а не с прошлыми доками.
- Capabilities conntrack могут отличаться от netmon (kprobes) — проверить
  фактически на Linux-хосте, а не переписывать вслепую из netmon.
