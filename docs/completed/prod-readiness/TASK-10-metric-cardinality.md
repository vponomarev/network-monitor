# TASK-10 — Управление кардинальностью netmon_tcp_loss_total

> ✅ **СТАТУС: ВЫПОЛНЕНО** (основная сессия). Реализовано:
> - Конфиг `metrics.cardinality.{level,max_series}` (дефолт `role`/`10000`), валидация в `config.go`.
> - Уровни лейблов `ip|role|network` (`labelNamesForLevel`/`labelsFor`), CounterVec строится под уровень.
> - Внутренний учёт (`exporter.series`) переключён с ключа-пары-IP на **ключ-серии** — ограничивает и число серий Prometheus, и рост памяти.
> - Лимит `max_series`: сверхлимитные новые серии дропаются, растёт `netmon_loss_series_dropped_total`, лог rate-limited (1/30с). Есть `netmon_loss_active_series`.
> - `SetMatchers` (reload) пересобирает серии с мержем коллизий; для агрегированных уровней лейблы восстанавливаются из репрезентативного IP пары (документированное приближение, TTL лечит).
> - **Побочно исправлен латентный баг:** в проде регистрируется сам `CounterVec`, а не `Exporter`, поэтому `Collect`/`cleanupOld` не вызывались — TTL не работал. Добавлен `Exporter.StartJanitor(ctx, interval)`, вызывается из `main.go`.
> - `NewExporter`/`NewExporterWithRegistry` оставлены как ip-level/unbounded (обратная совместимость — все старые тесты живы); прод использует `NewExporterWithConfig`.
> - Тесты: role-level без IP-лейблов, network-агрегация по /24, лимит `max_series` + счётчик дропов.
> - Примеры конфигов (`config.example.yaml`, `netmon.yaml.example`) обновлены.

**Метка исполнителя:** 🧠 strong
**Зависит от:** TASK-01 (сначала починить сам счётчик)
**Оценка:** ~1 день

---

## Контекст (проблема)

Метрика `netmon_tcp_loss_total` (`internal/metrics/exporter.go`) имеет лейблы,
включая **полные `src_ip` и `dst_ip`** плюс ещё 8 лейблов
(`src_location, dst_location, src_role, dst_role, src_network, dst_network, src_vrf, dst_vrf`).

В реальной сети число уникальных пар (src_ip, dst_ip) огромно → неограниченный рост
числа серий → OOM Prometheus и деградация scrape. Это классический «убийца» прод-инсталляции.

TTL-очистка (`cleanupOld`) частично помогает (после TASK-01 она удаляет серии), но
не защищает от пикового всплеска кардинальности.

## Что сделать

Реализовать управление кардинальностью с двумя рычагами: **жёсткий лимит серий** и
**режим агрегации** (конфигурируемый уровень детализации лейблов).

### 1. Конфиг

Добавь секцию (в `internal/config/config.go`, в `MetricsConfig` или новую подсекцию):
```go
type MetricsConfig struct {
    Name           string   `yaml:"name"`
    DefaultLabels  []string `yaml:"default_labels"`
    OptionalLabels []string `yaml:"optional_labels"`

    // Новое:
    Cardinality CardinalityConfig `yaml:"cardinality"`
}

type CardinalityConfig struct {
    // Уровень детализации: "ip" (по парам IP, как сейчас) |
    //                      "role" (агрегация до src_role/dst_role + locations, БЕЗ IP) |
    //                      "network" (агрегация до /24, БЕЗ полного IP)
    Level string `yaml:"level"`      // default: "role"
    // Жёсткий лимит числа активных серий. При превышении новые пары
    // сворачиваются в служебную серию "overflow" и растёт счётчик dropped.
    MaxSeries int `yaml:"max_series"` // default: 10000
}
```
- Дефолты: `Level: "role"`, `MaxSeries: 10000`.
- Валидация допустимых `Level`: `ip|role|network`.

> ⚠️ Смена дефолта на `role` МЕНЯЕТ набор лейблов метрики. Это осознанное
> production-решение (иначе прод падает). Обязательно задокументируй в TASK-13 и в
> примерах конфигов, и предупреди, что для сохранения старого поведения нужно
> `level: ip` (не рекомендуется для больших сетей).

### 2. Логика лейблов по уровню

В exporter при построении лейблов для серии:
- `level: "ip"` — как сейчас (полные src_ip/dst_ip + всё остальное).
- `level: "network"` — вместо полного IP использовать `src_network`/`dst_network`
  (уже вычисляется `getNetwork` → /24); `src_ip`/`dst_ip` НЕ включать как лейблы.
- `level: "role"` (default) — лейблы: `src_location, dst_location, src_role, dst_role,
  src_vrf, dst_vrf` (+ опционально network). БЕЗ `src_ip`/`dst_ip`.

Т.е. набор лейблов `CounterVec` определяется уровнем на этапе создания метрики.
Придётся сделать выбор набора лейблов в `NewExporterWithRegistry` в зависимости от
конфига (передавай `CardinalityConfig` в конструктор).

> Внутренний `events map[pairKey]` для TTL и API top-loss может по-прежнему хранить
> детализацию по IP — это память процесса, не серии Prometheus. Ограничивать надо
> именно число СЕРИЙ Prometheus. Но и рост `events` тоже ограничь `MaxSeries` (по
> числу уникальных ключей агрегации), чтобы не текла память.

### 3. Жёсткий лимит серий

- Веди счётчик активных серий (по числу уникальных наборов лейблов).
- При попытке создать новую серию сверх `MaxSeries`:
  - не создавай новую серию;
  - инкрементируй служебный счётчик `netmon_loss_series_dropped_total` (Counter);
  - (опционально) агрегируй в единую серию с меткой `overflow="true"`.
- Логируй превышение лимита не чаще раза в N секунд (rate-limit лог), чтобы не залить логи.

### 4. Новая служебная метрика

`netmon_loss_series_dropped_total` (Counter) — сколько пар не получили own-серию
из-за лимита. Плюс gauge `netmon_loss_active_series` — текущее число активных серий.

## Критерии приёмки (Definition of Done)

1. Конфиг `metrics.cardinality.level` управляет набором лейблов; дефолт `role` (без IP).
2. `level: ip` воспроизводит прежний набор лейблов (обратная совместимость по запросу).
3. При превышении `max_series` новые серии не создаются, растёт `netmon_loss_series_dropped_total`.
4. Есть `netmon_loss_active_series`.
5. TTL-очистка (TASK-01) уменьшает `active_series` при устаревании.
6. Unit-тесты: (a) число серий не превышает лимит; (b) для `role` в лейблах нет src_ip/dst_ip.
7. `go test ./...` зелёный.

## Как проверить
```bash
go test ./internal/metrics/...
# нагрузочно (Linux): сгенерировать много уникальных пар → active_series ≤ max_series,
# dropped_total растёт.
curl -s :9876/metrics | grep -E 'netmon_loss_active_series|netmon_loss_series_dropped_total'
curl -s :9876/metrics | grep netmon_tcp_loss_total | head   # проверить набор лейблов
```

## Риски
- Смена дефолтного набора лейблов ломает существующие дашборды — согласуй и задокументируй (TASK-13).
- Аккуратно с блокировками: подсчёт серий и создание идут под тем же мьютексом, что и `events`.
