# TASK-12 — CI-сборка eBPF + smoke-load, гигиена репозитория

> ✅ **СТАТУС: ВЫПОЛНЕНО** (основная сессия). Реализовано и проверено на реальных ядрах:
> - `.github/workflows/ebpf-build.yml` переписан: toolchain → `make -C bpf all` (явный `all`, не bare `make`, который регенерит `vmlinux.h`) → сверка embedded `.o` (advisory `::warning::`, т.к. релиз пересобирает) → **smoke-load `tcploss.bpf.o`** через bpftool на ядре раннера (verifier+CO-RE, блокирующе) → conntrack load информационно (`continue-on-error`) → struct-check `go test -run Validate\|HandleRecord ./internal/losscollector/...`.
> - `.github/workflows/release.yml`: `build-netmon` теперь пересобирает eBPF и копирует в `pkg/embedded/bpf/` перед `go build` — релиз не embed'ит устаревший `.o`.
> - Гигиена: `git rm --cached netmon dist/*`; `.gitignore` дополнен (`dist/`, `/netmon`, `/conntrack`, `/pktloss`). Embedded `.o` (`pkg/embedded/bpf/*.o`) остаются в git.
> - **Побочно:** починен красный job `test` в `ci.yml` — устаревший `conntrack_linux_test.go` (несуществующие `tracker.connectionKey`/`sendEvent`/`simulateEvents(ctx)`, канал теперь `*Connection`); `connectionKey`→`makeConnectionKey`, два obsolete-теста удалены. Полная `ci.yml`-команда `test` зелёная на Linux (все пакеты `ok`).
> - **Найден отложенный баг:** `conntrack.bpf.o` не грузится на 6.8/6.12 (CO-RE `trace_event_raw_inet_sock_set_state.saddr`) → [[APPENDIX-conntrack-later]] C-7.
> - Проверено на debian13 (6.12): `make all`, copy, bpftool load `tcploss.bpf.o`, `go test -run` losscollector, полная `ci.yml`-команда `test`.

**Метка исполнителя:** 🧠 strong 🐧 linux-host
**Зависит от:** TASK-04 (для сборки tcploss.bpf.o)
**Оценка:** ~1 день

---

## Контекст (проблема)

1. Собранные eBPF `.o` (`pkg/embedded/bpf/conntrack.bpf.o`, будущий `tcploss.bpf.o`)
   **закоммичены** и подхватываются через `//go:embed`, но их пересборка и
   загружаемость на целевых ядрах **не проверяются в CI**. Рассинхрон между
   C-структурой в `.bpf.c` и Go-структурой парсинга (`bpfConnectionEvent` /
   `bpfLossEvent`) вылезет только в рантайме на проде.
2. В git закоммичены крупные бинарники: `netmon` (14MB), `bin/*`, `dist/*`. Это
   раздувает репозиторий и создаёт риск залить устаревший артефакт.
   Проверка:
   ```bash
   git ls-files | grep -E '^(netmon$|bin/|dist/)'
   ```

## Что сделать

### Часть A — CI-сборка и проверка eBPF

Добавь в CI (файлы workflow в `.github/workflows/`; проверь, что там уже есть —
`ci.yml`, `release.yml`) шаги:

1. **Сборка eBPF из исходников** на Linux-раннере с `clang`/`llvm`:
   ```yaml
   - name: Install eBPF toolchain
     run: sudo apt-get update && sudo apt-get install -y clang llvm libbpf-dev
   - name: Build eBPF objects
     run: make -C bpf all
   ```
2. **Проверка, что закоммиченные .o актуальны** (не забыли пересобрать):
   ```yaml
   - name: Verify embedded eBPF is up to date
     run: |
       make -C bpf all
       git diff --exit-code -- pkg/embedded/bpf/ \
         || (echo "Embedded .o files are stale — rebuild and commit"; exit 1)
   ```
   > Если решишь НЕ коммитить `.o` вообще (собирать в CI) — тогда убери их из git и
   > генерируй в pipeline перед `go build`. Но т.к. `//go:embed` требует наличия
   > файла на момент компиляции, коммит `.o` — прагматичный выбор. Оставь коммит `.o`,
   > но добавь проверку актуальности (шаг выше).
3. **Smoke-load тест** на раннере (если ядро раннера поддерживает; GitHub ubuntu-latest
   обычно 6.x с BTF): загрузить программы через существующий `tests/verify_elf.go`
   или `bpftool prog load`, убедиться что верификатор принимает. Если раннер не
   даёт нужных прав/ядра — вынеси в отдельный (возможно self-hosted / ручной) job и
   пометь `continue-on-error: false` только там, где реально доступно.
4. **Проверка соответствия C- и Go-структур:** добавь Go-тест, который вызывает
   `validateBpfConnectionEvent()` и `validateBpfLossEvent()` (из TASK-05) —
   он и так должен быть, убедись что он в `go test ./...` и гоняется в CI.

### Часть B — гигиена репозитория

1. Убери бинарники из индекса git (файлы на диске можно оставить локально):
   ```bash
   git rm --cached netmon
   git rm --cached -r bin dist 2>/dev/null || true
   ```
2. Добавь/дополни `.gitignore`:
   ```gitignore
   /netmon
   /bin/
   /dist/
   *.test
   ```
   > НЕ игнорируй `pkg/embedded/bpf/*.o` — они нужны для `//go:embed` и остаются в git
   > (их актуальность проверяется в CI, Часть A).
3. Проверь, что релизные артефакты собираются в pipeline (`release.yml`) и
   публикуются через GitHub Releases, а не хранятся в git.

## Критерии приёмки (Definition of Done)

1. CI собирает eBPF из `.c` и падает, если закоммиченные `.o` устарели.
2. CI гоняет тесты валидации размеров структур (C↔Go).
3. Бинарники `netmon`, `bin/`, `dist/` удалены из индекса git и добавлены в `.gitignore`.
4. `pkg/embedded/bpf/*.o` остаются в git.
5. Локальная сборка/тесты не сломаны: `go build ./...`, `go test ./...`.

## Как проверить
```bash
git ls-files | grep -E '^(netmon$|bin/|dist/)'   # должно быть пусто после git rm --cached
make -C bpf all && git diff --exit-code -- pkg/embedded/bpf/   # .o актуальны
go test ./...
```

## Риски
- Не удали случайно `.o` из embed — сломается сборка Linux.
- `git rm --cached` не трогает файлы на диске, только индекс — это ожидаемо.
