# TASK-02 — Корректный longest-prefix match + починить красные тесты

**Метка исполнителя:** 👷 qwen-ok
**Зависит от:** нет
**Оценка:** ~1 час

---

## Контекст (проблема)

Разметка по ролям/локациям — ядро задачи. IP сопоставляется с самой специфичной
подсетью (longest prefix): `/32` должен побеждать `/22`.

Сейчас в `internal/metadata/role.go` (`GetRole`) и `internal/metadata/location.go`
(`GetLocation`, `GetVrf`) выбор реализован как «вернуть первое совпадение по
слайсу `m.networks`»:
```go
for _, nwr := range m.networks {
    if nwr.network.Contains(parsedIP) {
        return nwr.role   // первое совпадение, НЕ обязательно самое специфичное
    }
}
```
Корректность держится ТОЛЬКО на том, что `Load()` сортирует слайс по длине маски
(«most specific first»). Это хрупкий инвариант: если слайс наполнен не через
`Load()` (например, в тестах или в будущем коде) — матчинг вернёт неверный результат.

**Прямое следствие:** сейчас ПАДАЮТ тесты:
- `TestRoleMatcher_BestMatch` (`internal/metadata/matcher_test.go`)
- `TestLocationMatcher_BestMatch`

Они наполняют `m.networks` напрямую в порядке `/22, /32, /32` и ожидают, что
`GetRole("10.179.64.32")` вернёт роль из `/32`. Первое-совпадение возвращает `/22`.

Проверить факт падения:
```bash
go test ./internal/metadata/... -run BestMatch -v
```

## Что сделать

Сделать матчинг **независимым от порядка** слайса: выбирать запись с максимальной
длиной префикса среди всех, чья подсеть содержит IP.

### `internal/metadata/role.go` → `GetRole`
```go
func (m *RoleMatcher) GetRole(ip string) string {
    m.mu.RLock()
    defer m.mu.RUnlock()

    parsedIP := net.ParseIP(ip)
    if parsedIP == nil {
        return "unknown"
    }

    best := ""
    bestLen := -1
    for _, nwr := range m.networks {
        if nwr.network.Contains(parsedIP) {
            ones, _ := nwr.network.Mask.Size()
            if ones > bestLen {
                bestLen = ones
                best = nwr.role
            }
        }
    }
    if bestLen < 0 {
        return "unknown"
    }
    return best
}
```

### `internal/metadata/location.go`
Аналогично исправь `GetLocation` и `GetVrf` (если `GetVrf` тоже итерирует
`m.networks` и возвращает первое совпадение — применить тот же приём longest-prefix).
Прочитай файл целиком перед правкой и повтори паттерн выше для каждой функции,
возвращающей значение по best-match.

> Сортировку в `Load()` можно ОСТАВИТЬ (она безвредна и полезна для логов/детерминизма),
> но матчинг больше не должен от неё зависеть.

## Критерии приёмки (Definition of Done)

1. `TestRoleMatcher_BestMatch` и `TestLocationMatcher_BestMatch` — зелёные.
2. Матчинг возвращает самую специфичную подсеть НЕЗАВИСИМО от порядка `m.networks`.
3. Для IP вне всех подсетей возвращается `"unknown"` (как сейчас).
4. `go test ./...` — полностью зелёный (эти тесты были единственными красными).

## Как проверить
```bash
go test ./internal/metadata/... -v
go test ./...          # весь набор должен стать зелёным
```
Дополнительно добавь тест «порядок не важен»: тот же набор подсетей, но
перемешанный слайс — результат best-match не меняется.

## Риски
- Убедись, что `GetVrf` и любые другие best-match методы в `location.go` тоже
  переведены на longest-prefix, а не только `GetLocation`/`GetRole`.
