# TASK-11 — Настраиваемый bind-адрес + защита HTTP-эндпоинтов

**Метка исполнителя:** 👷 qwen-ok
**Зависит от:** нет
**Оценка:** ~2-3 часа

---

## Контекст (проблема)

HTTP-сервер netmon слушает `fmt.Sprintf(":%d", cfg.Global.MetricsPort)`
(`cmd/netmon/main.go:392`) — то есть на **всех интерфейсах** (`0.0.0.0`). Открыты:
`/metrics`, `/health`, `/ready`, а также API `/api/v1/...` (discovery, conntrack,
metadata). В проде это нежелательно: метрики и API доступны из сети без ограничений.

## Что сделать

### 1. Конфигурируемый bind-адрес

В `internal/config/config.go`, в `GlobalConfig`, добавь:
```go
MetricsAddr string `yaml:"metrics_addr"` // bind address, напр. "127.0.0.1" или "" (=все интерфейсы)
```
- Дефолт в `DefaultConfig()`: `MetricsAddr: ""` (сохраняем обратную совместимость —
  все интерфейсы). Но в примерах конфигов (`configs/*.example.yaml`) поставь
  `metrics_addr: 0.0.0.0` с комментарием-рекомендацией «для прода ограничьте до
  127.0.0.1 или адреса мониторинг-сети».
- В `main.go` строй адрес как `fmt.Sprintf("%s:%d", cfg.Global.MetricsAddr, cfg.Global.MetricsPort)`.
- Провалидируй `MetricsAddr` в `Validate()`: пустая строка ок; иначе `net.ParseIP`
  должен распарсить (или разреши hostname — тогда проверку смягчи). Достаточно
  проверки, что это либо "", либо валидный IP.

### 2. Опциональная защита эндпоинтов

Добавь опциональный bearer-token / basic-auth для НЕ-health эндпоинтов.
`/health` и `/ready` ДОЛЖНЫ остаться без аутентификации (их дёргают liveness/readiness пробы).

Конфиг:
```go
type GlobalConfig struct {
    // ...
    AuthToken string `yaml:"auth_token"` // если непусто — требовать Authorization: Bearer <token> на /metrics и /api/*
}
```
> ⚠️ Не логируй значение токена. В примерах конфига оставь `auth_token: ""`
> (выключено) и комментарий, что значение лучше подавать через переменную окружения.
> Поддержи чтение из env: если `AuthToken == ""`, читай `os.Getenv("NETMON_AUTH_TOKEN")`.

Реализация — middleware-обёртка:
```go
func requireAuth(token string, next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if token == "" { next.ServeHTTP(w, r); return } // auth выключен
        got := r.Header.Get("Authorization")
        if got != "Bearer "+token {
            w.WriteHeader(http.StatusUnauthorized)
            return
        }
        next.ServeHTTP(w, r)
    })
}
```
Оберни в неё `/metrics` и все `/api/v1/...` хендлеры, НЕ оборачивая `/health` и `/ready`.

> Сравнение токена делай через `crypto/subtle.ConstantTimeCompare`, чтобы не давать
> тайминг-атаку. Пример:
> `subtle.ConstantTimeCompare([]byte(got), []byte("Bearer "+token)) == 1`.

## Критерии приёмки (Definition of Done)

1. `metrics_addr` управляет bind-адресом; дефолт сохраняет прежнее поведение.
2. При заданном `auth_token` (или `NETMON_AUTH_TOKEN`) `/metrics` и `/api/*` требуют
   `Authorization: Bearer <token>`; без токена → `401`.
3. `/health` и `/ready` доступны всегда без токена.
4. Значение токена нигде не логируется.
5. Сравнение токена — константное по времени.
6. Тесты (`httptest`): 401 без токена, 200 с токеном, health/ready без токена.
7. `go build ./...`, `go test ./...` — зелёные.

## Как проверить
```bash
go test ./cmd/netmon/... 2>/dev/null || go test ./...
# руками:
NETMON_AUTH_TOKEN=secret ./netmon --config config.yaml &
curl -i :9876/metrics                                   # 401
curl -i -H "Authorization: Bearer secret" :9876/metrics # 200
curl -i :9876/ready                                     # 200 (без токена)
```

## Риски
- Не закрой health/ready токеном — сломаешь пробы оркестратора.
- Обратная совместимость: пустой auth_token = поведение как раньше (без auth).
