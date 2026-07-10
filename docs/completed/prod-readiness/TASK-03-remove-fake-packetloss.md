# TASK-03 — Изолировать нерабочий модуль internal/packetloss

**Метка исполнителя:** 👷 qwen-ok
**Зависит от:** нет
**Оценка:** ~30 мин

---

## Контекст (проблема)

Пакет `internal/packetloss` (`packetloss_linux.go`) и бинарь `cmd/pktloss` — это
**нерабочая заглушка**, которая выдаёт бессмысленные данные и может ввести
операторов в заблуждение в проде:

- `readTracePipe` матчит строки регэкспом `(\w+):.*(?:drop|loss|timeout|retransmit)`
  — ловит любые события ядра со словами drop/loss/timeout, не связанные с нужным интерфейсом.
- В `recordPacketLoss` поле `totalPackets` инкрементится ТОЛЬКО вместе с `lostPackets`,
  а окно `windowPackets` записывает только потери. Значит `calculateLossPercent`
  быстро уходит к 100% и там залипает — «процент потерь» не имеет физического смысла.
- Это НЕ тот путь, по которому netmon собирает реальные потери (реальный путь —
  `internal/collector` + `internal/metrics`, будущий eBPF-коллектор из TASK-04/05).

Оставлять этот модуль включаемым в проде нельзя.

## Что сделать

**Вариант по умолчанию (выбери его):** пометить модуль как экспериментальный и
гарантировать, что он НЕ участвует в проде.

1. Проверь, где используется пакет:
   ```bash
   grep -rn "internal/packetloss" --include=*.go .
   grep -rn "PacketLoss" cmd/netmon/main.go
   ```
   Убедись, что `cmd/netmon/main.go` НЕ запускает `packetloss.Monitor` (сейчас он
   его не запускает — loss там идёт через `collector.NewTracePipeCollector`).
   `packetloss` используется только в отдельном бинаре `cmd/pktloss`.

2. В начало файлов `internal/packetloss/packetloss_linux.go`,
   `internal/packetloss/packetloss_other.go` и `cmd/pktloss/main.go` добавь
   docstring-предупреждение уровня пакета:
   ```go
   // Package packetloss is EXPERIMENTAL and NOT production-ready.
   //
   // Its loss detection is a heuristic scrape of trace_pipe and does not produce
   // meaningful loss percentages. Do NOT enable it in production. Real TCP loss
   // metrics are produced by internal/collector + internal/metrics (see the
   // netmon binary). This package is kept only for experiments.
   package packetloss
   ```

3. В `cmd/pktloss/main.go` в `Long`-описании cobra-команды добавь префикс
   `"[EXPERIMENTAL — NOT production-ready] "`.

4. В `README.md` (корень репозитория) в разделе про модули убери/пометь `pktloss`
   как экспериментальный, чтобы он не выглядел как поддерживаемая фича.

5. Убедись, что сборка релиза (`Makefile`, `scripts/build*.sh`, CI) по умолчанию
   собирает `netmon` и `conntrack`, но НЕ распространяет `pktloss` как
   production-артефакт. Если `pktloss` сейчас в списке релизных бинарей — вынеси
   его или помести под явный `experimental`-таргет. Проверь:
   ```bash
   grep -rn "pktloss" Makefile scripts/ .github/ 2>/dev/null
   ```

> Альтернатива (НЕ выбирай без согласования с владельцем): полное удаление
> `internal/packetloss` и `cmd/pktloss`. Не делай этого в рамках этой задачи —
> достаточно изоляции.

## Критерии приёмки (Definition of Done)

1. `cmd/netmon` не импортирует и не запускает `internal/packetloss` (подтвердить grep'ом).
2. В коде пакета и в CLI-описании явно написано «EXPERIMENTAL / NOT production-ready».
3. Релизная сборка не публикует `pktloss` как обычный прод-бинарь (или он под experimental-таргетом).
4. `go build ./...` и `go test ./...` — зелёные.

## Как проверить
```bash
grep -rn "internal/packetloss" cmd/netmon/     # пусто
go build ./...
go test ./...
```

## Риски
- Не удаляй код без согласования — только изоляция и маркировка.
