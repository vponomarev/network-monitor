# TASK-09 — Прокидывание фатальных ошибок фоновых горутин

> ✅ **СТАТУС: ВЫПОЛНЕНО** (основная сессия). В `cmd/netmon/main.go`:
> - `setFatal(err)` — фиксирует первую фатальную ошибку (mutex) и вызывает `cancel()`.
> - Критичные компоненты: **loss collector** и **HTTP server** — при ошибке (кроме `context.Canceled`/`http.ErrServerClosed`) зовут `setFatal`; коллектор дополнительно сбрасывает readiness/`up`.
> - Некритичные (conntrack, bandwidth, latency, dns, discovery, metadata pollers) — только логируются.
> - После graceful shutdown: если `fatalErr != nil` → `logger.Sync()` + `os.Exit(1)` (supervisor перезапустит). Нормальный SIGTERM/SIGINT → `fatalErr == nil` → выход 0.
> - Проверка на Linux (отзыв прав/несуществующий bind → выход !=0) остаётся ручной (см. ниже) — main-orchestration юнит-тестами не покрывается без рефактора.

**Метка исполнителя:** 🧠 strong
**Зависит от:** TASK-06, TASK-07
**Оценка:** ~0.5 дня

---

## Контекст (проблема)

В `cmd/netmon/main.go` фоновые компоненты запускаются как «fire-and-forget»
горутины, а их фатальные ошибки только логируются:
```go
go func() {
    if err := collector.Run(ctx); err != nil {
        logger.Error("Collector error", zap.Error(err))
    }
}()
```
Если коллектор потерь (главный компонент netmon) навсегда упал — процесс
продолжает жить «пустым», `/health` рапортует OK. Оркестратор не перезапустит.

Нужно различать:
- **Критичные** компоненты (коллектор потерь) — их фатальная ошибка должна вести к
  завершению процесса с ненулевым кодом ИЛИ к `/ready=503`, чтобы оркестратор среагировал.
- **Некритичные** (bandwidth/latency/dns/discovery, а также conntrack на этом
  этапе) — ошибка логируется, но процесс живёт.

## Что сделать

### Вариант реализации (рекомендуемый): errgroup для критичных + readiness

1. Заведи для **критичных** компонентов механизм, который при их падении:
   - выставляет `hs.SetCollectorReady(false)` (из TASK-07);
   - инициирует управляемое завершение (`cancel()`), чтобы процесс вышел с
     ненулевым кодом и был перезапущен супервизором (systemd `Restart=on-failure`).

   Пример:
   ```go
   // критичный компонент
   go func() {
       err := lc.Run(ctx)
       hs.SetCollectorReady(false)
       if err != nil && !errors.Is(err, context.Canceled) {
           logger.Error("FATAL: loss collector stopped", zap.Error(err))
           fatalErr.Store(err) // sync/atomic.Value или отдельный канал
           cancel()            // завершаем приложение
       }
   }()
   ```
2. В конце `main` после `<-ctx.Done()` и graceful shutdown — если была зафиксирована
   фатальная ошибка критичного компонента, заверши процесс ненулевым кодом:
   ```go
   if err, ok := fatalErr.Load().(error); ok && err != nil {
       logger.Error("exiting with error", zap.Error(err))
       os.Exit(1)
   }
   ```
   > Важно: `os.Exit` не выполняет `defer`. Сделай `logger.Sync()` и
   > `server.Shutdown()` ДО `os.Exit`. Оформи так, чтобы shutdown-логика выполнилась,
   > а `os.Exit(1)` был самым последним.

3. **Некритичные** компоненты оставь как есть (лог + продолжение), но убедись, что
   их падение НЕ триггерит общий `cancel()`.

### Классификация компонентов

| Компонент | Критичность |
|-----------|-------------|
| Loss collector (eBPF/tracepipe) | **Критичный** |
| HTTP metrics server | **Критичный** (уже вызывает `cancel()` при ошибке — оставить) |
| conntrack tracker | Некритичный (на этом этапе) |
| bandwidth / latency / dns / discovery | Некритичный |
| metadata HTTP pollers | Некритичный |

## Критерии приёмки (Definition of Done)

1. Фатальная остановка коллектора потерь → `/ready` становится `503` И процесс
   завершается с кодом `1` (после graceful shutdown HTTP-сервера и `logger.Sync()`).
2. Падение некритичного компонента НЕ завершает процесс.
3. Нет утечки горутин; `ctx` отменяется корректно.
4. `go build ./...`, `go vet ./...`, `go test -race ./...` — зелёные.

## Как проверить
```bash
go build ./...
go vet ./...
# Linux: смоделировать фатальную ошибку коллектора (например, отобрать capability
# на eBPF) → процесс должен выйти с кодом !=0.
sudo -u nobody ./netmon --config config.yaml ; echo "exit=$?"   # ожидаем ненулевой код
```

## Риски
- Аккуратно с порядком `os.Exit` vs `defer logger.Sync()` — `os.Exit` игнорирует defer.
- Не допусти, чтобы обычная отмена по SIGTERM трактовалась как фатальная ошибка
  (фильтруй `context.Canceled`).
