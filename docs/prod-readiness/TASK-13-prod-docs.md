# TASK-13 — Документация production-развёртывания

> ✅ **СТАТУС: ВЫПОЛНЕНО** (основная сессия). `docs/PRODUCTION_en.md` и
> `docs/PRODUCTION_ru.md` выверены по реальному коду; `packaging/netmon.service`
> исправлен; ссылки добавлены в корневой `README.md`.
>
> **Ключевая находка (проверено на debian13, ядро 6.12):** hardened-юнит с
> `CAP_BPF CAP_PERFMON CAP_NET_RAW` НЕ делает attach трейспоинта
> `tcp_retransmit_skb` — `link.Tracepoint` → `perf_event_open` возвращает EPERM при
> `kernel.perf_event_paranoid > 1` (дефолт Debian/Ubuntu 3/4). Программа грузится
> (CAP_BPF), но не attach'ится. Прежние E2E проходили только потому, что бинарь
> запускался неограниченным root. Рекомендуемый набор изменён на
> **`CAP_SYS_ADMIN CAP_NET_RAW`** (split-набор оставлен опциональной least-privilege
> альтернативой с предупреждением). Проверено установкой реального юнита:
> `systemctl is-active`=`active`, `/ready`=200, `netmon_loss_collector_up 1`,
> CapEff/CapBnd = `0x202000` (CAP_SYS_ADMIN + CAP_NET_RAW).
>
> Также в `_ru.md` исправлено: имя метрики `netmon_loss_parse_errors_total`,
> порог ядра `5.8+`, метки по уровням кардинальности (`vrf` вместо ложного
> `network` на уровне `role`), добавлен алерт `NetmonReadNotParsed`.

**Метка исполнителя:** 👷 qwen-ok → 🧠 strong 🐧 linux-host (из-за находки по capabilities)
**Зависит от:** желательно после TASK-06, TASK-10, TASK-11 (чтобы документировать финальные конфиг-опции)
**Оценка:** ~0.5 дня

---

## Контекст (зачем)

Нужен единый, точный документ по прод-развёртыванию netmon для сбора потерь TCP с
разметкой по ролям. Сейчас документация разрознена и частично описывает
trace_pipe-путь, который заменяется на eBPF (TASK-04..06).

## Что сделать

Создай `docs/PRODUCTION.md` (и добавь ссылку на него из корневого `README.md`).
Документ должен покрывать:

### 1. Требования к среде
- Linux-ядро: **5.14+** (для трейспоинта `tcp_retransmit_skb` + ring buffer через CO-RE).
- BTF включён: файл `/sys/kernel/btf/vmlinux` должен существовать.
- Права/capabilities для eBPF-пути. Проверь и укажи фактически необходимые:
  `CAP_BPF`, `CAP_PERFMON` (на 5.8+), либо `CAP_SYS_ADMIN` на старых ядрах. Уточни
  из кода/тестов, что реально требуется для `link.Tracepoint` + ringbuf.
- НЕ требуется root для чтения текстового trace_pipe, если используется eBPF-путь
  (в этом преимущество перед старым источником).

### 2. Конфигурация (актуальная после Фазы 1–3)
Задокументируй новые/изменённые опции:
- `global.loss_source: ebpf|tracepipe` (дефолт `ebpf`) — TASK-06.
- `global.metrics_addr`, `global.auth_token` (+ env `NETMON_AUTH_TOKEN`) — TASK-11.
- `metrics.cardinality.level: ip|role|network` (дефолт `role`) и `metrics.cardinality.max_series` — TASK-10.
  **Явно предупреди:** дефолт `role` НЕ включает `src_ip`/`dst_ip` в лейблы; для
  старого поведения нужен `level: ip` (не рекомендуется в больших сетях; риск OOM Prometheus).
- Разметка по ролям/локациям: формат `roles.yaml` и `locations.yaml` (см. примеры
  в `configs/`), правило **longest-prefix** (`/32` побеждает `/22`) — TASK-02.
- HTTP-обновление метаданных (`update_source`) — уже описано в корневом README, дай ссылку.

### 3. Пример systemd-юнита
Возьми за основу `packaging/netmon.service` (проверь его актуальность) и приведи
пример с:
- `Restart=on-failure` (важно вместе с TASK-09 — при фатальной ошибке коллектора
  процесс выходит с кодом !=0 и systemd перезапускает);
- нужными capabilities (`AmbientCapabilities=CAP_BPF CAP_PERFMON` и т.п.);
- ограничением ресурсов (память).

### 4. Health/Readiness
- `/health` — liveness (всегда 200, если процесс жив).
- `/ready` — readiness (200 только когда коллектор собирает данные; иначе 503) — TASK-07.
- Пример liveness/readiness проб для systemd/k8s.

### 5. Метрики и алерты
- Основная метрика `netmon_tcp_loss_total` (после TASK-01 считается корректно, +1 на ретрансмит).
- Метрики самонаблюдения `netmon_loss_collector_up`, `netmon_loss_events_*` — TASK-08.
- Метрики кардинальности `netmon_loss_active_series`, `netmon_loss_series_dropped_total` — TASK-10.
- Готовые PromQL-алерты:
  ```promql
  netmon_loss_collector_up == 0                                  # сбор сломан
  rate(netmon_loss_events_read_total[5m]) > 0
    and rate(netmon_loss_events_parsed_total[5m]) == 0           # формат событий сломан
  netmon_loss_series_dropped_total > 0                           # упёрлись в лимит кардинальности
  ```

### 6. Ограничения
- IPv4-only (IPv6 не поддерживается на этом этапе).
- Ретрансмит — прокси-метрика потерь, не абсолютный подсчёт потерянных пакетов.

## Критерии приёмки (Definition of Done)

1. `docs/PRODUCTION.md` создан и покрывает разделы 1–6.
2. Все конфиг-опции соответствуют реально реализованным в коде (сверься с `config.go`
   ПОСЛЕ соответствующих задач; не документируй то, чего нет).
3. Ссылка на `PRODUCTION.md` добавлена в корневой `README.md`.
4. Устаревшие упоминания «loss через trace_pipe как основной путь» помечены как legacy.

## Как проверить
- Пройди по документу и сверь каждую опцию с `internal/config/config.go`.
- Проверь, что примеры команд (`curl`, `systemctl`) синтаксически корректны.

## Риски
- Не документируй фичи, которые ещё не реализованы (сверяйся с фактическим состоянием кода).
