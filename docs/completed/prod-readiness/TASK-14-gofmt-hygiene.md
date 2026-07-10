# TASK-14 — Привести репозиторий к gofmt

**Метка исполнителя:** 👷 qwen-ok
**Зависит от:** нет
**Оценка:** ~15 минут

---

## Контекст (проблема)

Общий Definition of Done требует `gofmt -l .` = пустой вывод. Сейчас 10 файлов не
отформатированы (это НЕ связано с TASK-08/11 — это накопленный до плана долг):

```
cmd/conntrack/install.go
cmd/conntrack/main.go
internal/conntrack/api.go
internal/conntrack/conntrack_linux_test.go
internal/conntrack/state_machine.go
internal/conntrack/state_machine_test.go
internal/conntrack/syslog_test.go
internal/metadata/http_poller.go
internal/metadata/validators_test.go
internal/packetloss/packetloss_extended_test.go
```

## Что сделать

1. Отформатировать все файлы:
   ```bash
   gofmt -w .
   ```
   (или точечно по списку выше — результат тот же).

2. Убедиться, что изменения — ТОЛЬКО форматирование (отступы, выравнивание,
   группировка импортов). НИКАКИХ правок логики. Просмотри diff:
   ```bash
   git diff --stat
   git diff        # бегло проверить, что это только whitespace/выравнивание
   ```

3. Проверить, что ничего не сломалось:
   ```bash
   go build ./...
   go vet ./...
   go test ./...
   gofmt -l .      # должно быть ПУСТО
   ```

## Критерии приёмки (Definition of Done)

1. `gofmt -l .` — пустой вывод.
2. `git diff` содержит только изменения форматирования, без правок логики.
3. `go build ./...`, `go vet ./...`, `go test ./...` — зелёные.

## Ограничения / риски

- Не трогай логику, не переименовывай, не удаляй код — только `gofmt -w`.
- Не форматируй сгенерированные/бинарные артефакты (gofmt и так работает только с `.go`).
- Не пересекается с активными задачами (TASK-09/10 правят `main.go`/`config.go`/
  `exporter.go` — их в списке gofmt нет; но если возникнет конфликт, приоритет у
  ветки с логикой, gofmt повторишь после).
