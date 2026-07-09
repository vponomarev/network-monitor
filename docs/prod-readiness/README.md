# Production-Readiness Plan — netmon (TCP loss + role labeling)

> **Цель проекта на этот план:** довести приложение **netmon** до production-ready
> состояния для сбора метрик **потерь TCP-пакетов** (ретрансмиты) с разметкой
> src/dst по **ролям и локациям** (конфиг-файлы с масками IP: `roles.yaml`,
> `locations.yaml`). Функционал conntrack (общий трекинг соединений через eBPF)
> нужен позже — он остаётся в том же бинаре, но НЕ в фокусе этого плана.

---

## Как пользоваться этим планом (важно для исполнителя)

Этот каталог — набор **независимых, самодостаточных задач**. Каждая задача лежит
в отдельном файле `TASK-NN-*.md` и содержит:

- **Контекст** — зачем это нужно (проблема в текущем коде).
- **Файлы** — точные пути и текущие номера строк.
- **Что сделать** — пошаговые инструкции, часто с примерами кода.
- **Критерии приёмки (Definition of Done)** — проверяемые условия.
- **Как проверить** — конкретные команды.
- **Ограничения / риски**.

Каждая задача помечена меткой **исполнителя**:

| Метка | Значение |
|-------|----------|
| `👷 qwen-ok` | Механическая, локальная задача. Можно отдать более слабой LLM. Всё нужное описано в файле. |
| `🧠 strong` | Нужна сильная модель: архитектурные решения, eBPF/ядро, неочевидные крайние случаи. |
| `🐧 linux-host` | Требует Linux-хост с ядром 5.14+, root, `clang/llvm`, `/sys/kernel/btf/vmlinux` для сборки/проверки eBPF. На macOS не проверяется. |

---

## Порядок выполнения (фазы)

Задачи внутри фазы можно делать параллельно, если у них нет `Зависит от`.
Фазы идут по возрастанию номера.

### Фаза 0 — Стабилизация (быстрые критичные фиксы)
- [`TASK-01`](TASK-01-fix-loss-counter.md) — ✅ **DONE** (qwen) — Исправлен подсчёт метрики потерь (`.Inc()` вместо `.Add(count)`) + утечка серий Prometheus.
- [`TASK-02`](TASK-02-longest-prefix-match.md) — ✅ **DONE** (qwen) — Longest-prefix match для ролей/локаций; красные тесты починены.
- [`TASK-03`](TASK-03-remove-fake-packetloss.md) — ✅ **DONE** (qwen) — Изолирован нерабочий модуль `internal/packetloss`.

### Фаза 1 — Production-путь сбора потерь (eBPF вместо trace_pipe)
- [`TASK-04`](TASK-04-ebpf-retransmit-program.md) — ✅ **DONE** (я) — `bpf/tcploss.bpf.c` на трейспоинт `tcp/tcp_retransmit_skb` + ring buffer `loss_events`, IPv4, без `bpf_printk`. Собран, `.o` в `pkg/embedded/bpf/`. **Load-тест пройден на 4 ядрах** (5.15/6.1/6.8/6.12, CO-RE).
- [`TASK-05`](TASK-05-ebpf-loss-collector.md) — ✅ **DONE** (я) — `internal/losscollector` (ebpf_linux.go + _other.go stub), ringbuf-reader без busy-loop (`ErrClosed`), парсинг с валидацией размера, self-метрики. Unit-тесты (linux) зелёные.
- [`TASK-06`](TASK-06-wire-collector-and-config.md) — ✅ **DONE** (я) — `global.loss_source: ebpf|tracepipe` (дефолт ebpf); проводка в `main.go`. **E2E на 4 ядрах**: обе ветки дают `netmon_tcp_loss_total` с разметкой по ролям, `read==parsed`, `parse_errors=0`.

### Фаза 2 — Надёжность и наблюдаемость
- [`TASK-07`](TASK-07-real-health-ready.md) — ✅ **DONE** — Реальные `/health` и `/ready`. Реализовано и развязано от TASK-06 (готовность привязана к текущему коллектору; `internal/health` переиспользуется eBPF-коллектором позже).
- [`TASK-08`](TASK-08-collector-self-metrics.md) — ✅ **DONE** (qwen) — `internal/collector/metrics.go`: `netmon_loss_collector_up`, `..._events_read_total`, `..._events_parsed_total`, `..._parse_errors_total`, `netmon_loss_source_info`. `up` симметрично 1/0 при старте/остановке. Покрыто тестами.
- [`TASK-09`](TASK-09-fatal-goroutine-errors.md) — ✅ **DONE** (я) — `setFatal` фиксирует первую ошибку критичных компонентов (loss collector, HTTP server) → `cancel()` → graceful shutdown → `os.Exit(1)` (после `logger.Sync()`). Некритичные (conntrack/bandwidth/latency/dns/discovery/pollers) только логируются. Нормальный SIGTERM → выход 0.

### Фаза 3 — Кардинальность и безопасность
- [`TASK-10`](TASK-10-metric-cardinality.md) — ✅ **DONE** (я) — `metrics.cardinality.level` (ip/role/network, дефолт **role**) + `max_series` (дефолт 10000). Новые метрики `netmon_loss_active_series`, `netmon_loss_series_dropped_total`. Внутр. учёт переключён на ключ-серии (ограничивает и память). Добавлен `StartJanitor` (TTL в проде реально работает — раньше `Collect` не вызывался). Примеры конфигов обновлены.
- [`TASK-11`](TASK-11-http-bind-and-auth.md) — ✅ **DONE** (qwen) — `metrics_addr` (валидация IP) + `auth_token` (env `NETMON_AUTH_TOKEN`, `subtle.ConstantTimeCompare`); `/metrics` и `/api/*` под auth, `/health`+`/ready` — нет.

### Фаза 4 — Сборка, CI, документация
- [`TASK-12`](TASK-12-ci-ebpf-build-and-hygiene.md) — ✅ **DONE** (я) — `ebpf-build.yml` переписан: build из исходников + smoke-load `tcploss.bpf.o` на ядре раннера + struct-check. `release.yml` пересобирает eBPF перед `go build` (нет stale embed). Бинарники (`netmon`, `dist/`) убраны из git, `.gitignore` дополнен. Побочно: починен красный `ci.yml` (устаревший conntrack-тест) и задокументирован CO-RE-баг conntrack на новых ядрах ([[APPENDIX-conntrack-later]] C-7/C-8).
- [`TASK-13`](TASK-13-prod-docs.md) — ✅ **DONE** (я) — Документация прод-развёртывания (`docs/PRODUCTION_en.md` + `_ru.md`) выверена по реальному коду: набор меток по уровням кардинальности (`role`/`network`/`ip`), имена метрик, ядро **5.8+** (проверено 5.15/6.1/6.8/6.12). **Ключевая находка:** hardened-юнит с `CAP_BPF CAP_PERFMON` НЕ делает attach трейспоинта (`perf_event_open` EPERM при `perf_event_paranoid>1`) — рекомендуемый набор изменён на `CAP_SYS_ADMIN CAP_NET_RAW`, проверено на debian13 (6.12): юнит `active`, `/ready`=200, `netmon_loss_collector_up 1`.

### Дополнительные задачи
- [`TASK-14`](TASK-14-gofmt-hygiene.md) — ✅ **DONE** (qwen) — Репозиторий приведён к `gofmt` (10 файлов). `gofmt -l .` пуст.

### Приложение
- [`APPENDIX-conntrack-later.md`](APPENDIX-conntrack-later.md) — Отложенные задачи по conntrack (не для этого этапа, чтобы не потерять).

---

## Рабочее соглашение (действует для ВСЕХ задач)

**Язык кода/комментариев:** как в окружающем коде. Комментарии в этом проекте —
смесь английского и русского; новые комментарии пиши по-английски в новом коде Go,
допускается русский в местах, где он уже используется. Не переписывай стиль соседнего кода.

**Сборка и тесты (запускать из корня репозитория):**
```bash
go build ./...            # должно собираться без ошибок
go vet ./...              # без предупреждений в изменённых пакетах
go test ./...             # ВСЕ тесты зелёные (сейчас есть красные — см. TASK-02)
gofmt -l .                # пустой вывод (нет неотформатированных файлов)
```
> ⚠️ eBPF-код (`bpf/*.c`, пакет `internal/conntrack`, `pkg/embedded`) собирается
> только под Linux с тегом сборки `linux`. На macOS `go build ./...` соберёт
> заглушки `*_other.go`. Изменения в eBPF проверяются на Linux-хосте (метка `🐧`).

**Definition of Done для любой задачи:**
1. `go build ./...` и `go vet ./...` проходят.
2. `go test ./...` — зелёный (или явно указано, какие тесты добавлены/изменены).
3. `gofmt -l .` пуст.
4. Выполнены все «Критерии приёмки» из файла задачи.
5. Не сломаны публичные сигнатуры, используемые в `cmd/netmon/main.go`, если задача этого прямо не требует.
6. Изменения минимальны и точечны — не рефакторить несвязанный код.

**Что НЕ делать без согласования:**
- Не менять формат/имена существующих Prometheus-метрик, кроме случаев, прямо описанных в задаче (это ломает дашборды).
- Не удалять существующие HTTP-эндпоинты.
- Не коммитить бинарные артефакты (см. TASK-12).

---

## Глоссарий

| Термин | Значение |
|--------|----------|
| **Потеря TCP / loss** | В этом проекте — событие ретрансмита сегмента (`tcp_retransmit_skb`). Это прокси-метрика потерь на пути. |
| **trace_pipe** | Файл `/sys/kernel/tracing/trace_pipe` — текстовый поток событий ядра. Единственный потребитель, глобальный ресурс. Текущий (нежелательный) источник данных. |
| **ring buffer (ringbuf)** | eBPF-механизм передачи структурированных событий из ядра в userspace без гонок за общий текстовый поток. Целевой источник данных. |
| **CO-RE** | Compile Once – Run Everywhere: eBPF, переносимый между ядрами через BTF (`/sys/kernel/btf/vmlinux`). |
| **best-match / longest-prefix** | Выбор самой специфичной подсети (макс. длина маски), содержащей IP. `/32` побеждает `/22`. |
| **role / location matcher** | `internal/metadata` — сопоставление IP → роль/локация по спискам подсетей из YAML. |
| **exporter** | `internal/metrics/exporter.go` — агрегирует события потерь и публикует Prometheus-метрику `netmon_tcp_loss_total`. |
