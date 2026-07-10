# TASK-07 — HTTP-сервер для standalone conntrack (`/metrics`, `/health`, `/ready`, auth)

**Метка исполнителя:** 🧠 strong
**Зависит от:** TASK-06 (метрики), желательно TASK-08 (readiness завязан на состояние коллектора)
**Оценка:** ~1 день

---

## Контекст (проблема)

Отдельный бинарь `cmd/conntrack` **не поднимает HTTP-сервер вообще** — нет
`/metrics`, `/health`, `/ready`, auth (grep по `ListenAndServe`/`promhttp` в
`cmd/conntrack` и `internal/conntrack` пуст). Prometheus-метрики регистрируются,
но недоступны, если conntrack не встроен в netmon. Для прод-эксплуатации
standalone conntrack нужны те же эндпоинты, что и у netmon.

netmon-образец — `cmd/netmon/main.go`:
- `/metrics` через `promhttp` с опциональным bearer-token auth (`:432-453`);
- `/health` (liveness, всегда 200) и `/ready` (readiness, 503 пока не готов) —
  `:458-459`, без auth;
- готовность привязана к состоянию коллектора (`internal/health`).

## Что сделать

1. В `cmd/conntrack` (или в новом `internal/conntrack` HTTP-хелпере) поднять
   `http.Server` на настраиваемом адресе/порте (флаг/конфиг, дефолт напр.
   `:9877`, чтобы не конфликтовать с netmon `:9876`).
2. Смонтировать:
   - `/metrics` — `promhttp.HandlerFor(registry, ...)` (использовать тот же
     registry, что и метрики TASK-06);
   - `/health` — `internal/health` `LivenessHandler()` (всегда 200);
   - `/ready` — `ReadinessHandler()` (503 пока ридер не запущен/упал);
   - существующий conntrack API (`internal/conntrack/api.go`
     `/api/v1/conntrack/connections`, `/stats`) — смонтировать и здесь.
3. Опциональный auth-токен для `/metrics` и `/api/*` (переиспользовать паттерн
   netmon: `crypto/subtle.ConstantTimeCompare`, env `CONNTRACK_AUTH_TOKEN` /
   поле конфига), `/health` и `/ready` — без auth.
4. Привязать `health.State` к жизненному циклу коллектора: `SetReady()` после
   успешного attach+старта ридера; сбрасывать при фатальной ошибке (TASK-08).
5. Переиспользовать `internal/health` — не дублировать логику.

## Критерии приёмки (DoD)
1. `conntrack --config ...` (standalone) отдаёт `/metrics`, `/health`, `/ready`
   на настроенном порту.
2. `/ready` = 503 до готовности ридера, 200 после; `/health` = 200 всегда при
   живом процессе.
3. Auth (если задан токен) закрывает `/metrics` и `/api/*`, не трогает
   `/health`/`/ready`.
4. Метрики conntrack (TASK-06) реально видны в standalone `/metrics`.
5. Встроенный режим netmon не сломан (там свой сервер; conntrack не должен
   поднимать второй на том же порту).
6. `go build ./... && go test ./... && gofmt -l .` — чисто.

## Как проверить
```bash
sudo ./conntrack --config /etc/conntrack/config.yaml &
curl -s -o /dev/null -w '%{http_code}\n' localhost:9877/health   # 200
curl -s -o /dev/null -w '%{http_code}\n' localhost:9877/ready    # 200 после старта
curl -s localhost:9877/metrics | grep -E '^conntrack_'
```

## Риски
- Порт по умолчанию не должен конфликтовать с netmon (`:9876`). Задокументировать.
- В embedded-режиме (внутри netmon) НЕ поднимать второй сервер — сделать запуск
  HTTP условным (только для standalone `cmd/conntrack`).
- Согласовать registry между TASK-06 и этим сервером (без двойной регистрации).
