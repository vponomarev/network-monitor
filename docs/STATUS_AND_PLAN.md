# Network Monitor — статус и план развития

*Актуализировано: 2026-07-18*

## Поддерживаемый scope

| Компонент | Назначение | Статус |
|---|---|---|
| `netmon` / `tcploss` | Мониторинг TCP-ретрансмитов | Production-ready |
| standalone `conntrack` | Трекинг жизненного цикла TCP-соединений | Production-qualified в v2.3.0 |
| `pktloss` | Legacy trace_pipe-прототип | Experimental, не для production |

Production-поставка поддерживает только Linux `amd64`. ARM-артефакты не
публикуются: для них нет архитектурно корректной eBPF qualification и доступных
runtime-хостов. Conntrack пока выпускается отдельным бинарником и systemd unit;
включение в единый daemon не входит в текущий релиз.

## Закрытые задачи

### Netmon

- eBPF ring buffer для `tcp_retransmit_skb`, CO-RE и C↔Go layout checks;
- наблюдаемые kernel/userspace drops и ограничение cardinality/TTL;
- реальные `/health` и `/ready`, bearer auth и graceful shutdown;
- production systemd, документация и release pipeline;
- verifier/runtime-проверки на ядрах 5.15, 6.1, 6.8 и 6.12.

### Conntrack

- исправлены CO-RE layout и загрузка на ядрах 5.15–6.12;
- удалены `bpf_printk`, busy-loop и production simulation;
- добавлены kernel/userspace drop metrics;
- PID/comm фиксируются без синхронного чтения `/proc`;
- incoming/outgoing `ESTABLISHED` и `CLOSED` имеют полный tuple и не дублируются;
- реализованы `/health`, `/ready`, `/metrics`, embedded eBPF, config и systemd;
- пройден обратимый `install → start → ready → deinstall`;
- Docker build публикует проверенный `linux/amd64` image;
- добавлены `tests/conntrack/e2e/run-host.sh` и `run-matrix.ps1` для повторяемой
  квалификации одного release-кандидата на всей матрице ядер.

Проверенная матрица:

| Хост | ОС | Ядро |
|---|---|---|
| `192.168.5.217` | Ubuntu 22.04 | `5.15.0-185` |
| `192.168.5.193` | Debian 12 | `6.1.0-45` |
| `192.168.5.99` | Proxmox VE 8 / Debian 12 | `6.8.12-20-pve` |
| `192.168.5.214` | Debian 13 | `6.12.85` |

## Релиз v2.2.0 — завершён 2026-07-13

1. [x] Убрать ARM из GitHub Release и документации поставки.
2. [x] Прогнать новый E2E-скрипт одним `conntrack-linux-amd64` артефактом на
   четырёх поддерживаемых ядрах — PASS на 5.15, 6.1, 6.8 и 6.12.
3. [x] Собрать release-кандидат на Linux-хосте, проверить установку из bundle,
   readiness, метрики, трафик, restart и deinstall.
4. [x] После успешной qualification выпустить новую minor-версию `v2.2.0`.

Финальные netmon/conntrack binaries и bundles собраны на Debian 13 из commit
`a7adb3b`; опубликованные GitHub assets сверены по SHA256. Release workflow,
post-merge CI, Security Scan и Docker Publish прошли успешно.

Alerts относятся к внешнему контуру мониторинга и не блокируют этот релиз.

## Релиз v2.3.0 — завершён 2026-07-18

Retention, жёсткие лимиты состояния, soak automation и безопасный
upgrade/rollback выпущены в `v2.3.0`. Финальные Linux `amd64` binaries и bundles
собраны на Debian 13 из merge-коммита `838089c`. Conntrack bundle прошёл полный
lifecycle, а один бинарник — ядра 5.15, 6.1, 6.8 и 6.12. Опубликованные assets
заменены квалифицированными host-built файлами и повторно сверены по SHA256.
Release workflow, post-merge CI, eBPF verifier, Security Scan и Docker Publish
прошли успешно.

## Закрытый P0 scope v2.3.0

### P0.1 — retention и ограничение состояния

- [x] TTL 24 часа для незакрытых соединений и записей после потерянного `CLOSE`;
- [x] настраиваемые жёсткие лимиты kernel correlation maps и userspace state;
- [x] периодическая очистка без goroutine на каждое закрытое соединение;
- [x] метрики текущего размера, cleanup, eviction и overflow;
- [x] unit/race/verifier и E2E одним бинарником на ядрах 5.15, 6.1, 6.8 и 6.12.

Defaults: `state_ttl: 24h`, `cleanup_interval: 1m`, не более 10240 tracked и
16384 pending kernel entries.

### P0.2 — эксплуатационная квалификация (завершена)

- [x] автоматизированный soak-профиль с пределами CPU, RSS, drops и размера состояния;
- [x] короткая проверка профиля на ядре 6.12: 20 секунд, RSS 52720 KiB,
  CPU 9.7%, 21 state entry, без новых drops;
- [x] 30-минутный прогон на ядре 6.12: RSS 55576 KiB, CPU 10.6%,
  максимум 433 aggregate state entries, без новых drops;
- [x] детерминированные тесты вытеснения при заполнении userspace-лимита и
  TTL-очистки userspace/kernel state при длительном отсутствии `CLOSE`.

### P0.3 — безопасный upgrade и rollback

- [x] сохранение существующей конфигурации и проверка её совместимости;
- [x] атомарная установка и повторная установка поверх работающей версии;
- [x] автоматическое восстановление сервиса после неуспешного обновления;
- [x] явная команда `conntrack rollback` с проверяемым rollback snapshot;
- [x] изолированный E2E для upgrade, reinstall, explicit и automatic rollback.

## Активный план развития

Приоритет после `v2.3.0` — закрепить эксплуатацию уже выпущенных `netmon` и
standalone `conntrack`. Новые collectors и объединение сервисов начинаются
только после закрытия инфраструктурного P0. Alerts относятся к внешнему контуру
мониторинга и остаются вне scope проекта.

### P0 — инфраструктура релиза и управляемость

#### P0.1 — обновление GitHub Actions — выполнено 2026-07-18

- first-party actions переведены на актуальные major с Node.js 24;
- CodeQL Action обновлён с v3 до v4, hosted toolchain — до Go 1.26;
- lint и `govulncheck` стали blocking gates, а CI Summary учитывает все jobs;
- eBPF build, verifier smoke-load, race, security и release checks сохранены;
- gosec SARIF публикует реальный legacy baseline вместо пустого отчёта.

Проверено в PR #14: CI, eBPF, CodeQL, govulncheck и gosec проходят без
предупреждений о deprecated Node.js runtime.

#### P0.2 — стабильный observability contract

- зафиксировать поддерживаемые имена метрик, labels и значения `reason/layer`;
- добавить regression-тесты публичной схемы Prometheus и HTTP endpoints;
- описать правила совместимости dashboards и миграцию при breaking changes;
- отделить внутренние diagnostic metrics от стабильного operator API.

**Готово, когда:** случайное переименование или изменение labels блокируется
тестами, а версия схемы и правила миграции отражены в документации.

#### P0.3 — production rollout runbook conntrack

- описать staged rollout `v2.2.x → v2.3.x`, проверку readiness и rollback;
- зафиксировать acceptance criteria по drops, state size, RSS и CPU;
- добавить краткий post-upgrade checklist и сбор диагностического bundle;
- проверить runbook на одном canary-хосте без изменения release binary.

**Готово, когда:** оператор может обновить, проверить и откатить conntrack по
одному воспроизводимому сценарию без знания внутреннего устройства сервиса.

### P1 — надёжность и hardening (после P0)

- расширенные lifecycle/API/concurrency/error-path тесты conntrack;
- нагрузочная и saturation qualification: заполнение maps/queues, длительное
  отсутствие `CLOSE`, стабильность retention и отсутствие неограниченного роста;
- усиление systemd sandbox и переход с `CAP_SYS_ADMIN` на минимальные capability
  отдельно для каждого ядра поддерживаемой матрицы;
- проверка совместимости конфигурации между minor-версиями и аварийных сценариев
  повреждённого rollback snapshot;
- генерация SBOM и provenance/signing для release assets.

Расширенные и нагрузочные тесты, а также systemd hardening остаются в roadmap,
но не входят в ближайший P0.

### P2 — развитие функциональности

#### P2.1 — IPv6

- IPv6 lifecycle tracking для conntrack и retransmit tracking для netmon;
- C↔Go layout checks, контроль cardinality и dashboards для IPv6 labels;
- verifier/runtime qualification на всей поддерживаемой матрице ядер.

#### P2.2 — единый управляемый daemon

- объединить `netmon` и `conntrack` под одним lifecycle и конфигурацией;
- сохранить feature flags, изоляцию отказов collectors и наблюдаемость drops;
- определить совместимый переход со standalone conntrack и независимый rollback.

#### P2.3 — container orchestration

- актуализировать container deployment guide и Compose;
- Kubernetes/Helm начинать только после стабилизации единого daemon lifecycle;
- явно документировать privileged/capability и host-kernel requirements.

#### P2.4 — дополнительные модули

- bandwidth, latency, DNS и topology включать в production scope по одному;
- для каждого модуля требовать реальные данные, bounded cardinality, health,
  документацию и отдельный qualification profile.

### P3 — условные направления

- ARM64 возвращается в план только после появления реального Linux ARM-хоста,
  отдельной архитектурной eBPF-сборки и полной runtime qualification;
- расширение kernel matrix выполняется при появлении production-хоста с новым
  LTS/дистрибутивным ядром, а не только на основании cross-compilation.

## Порядок ближайших работ

1. P0.2 — зафиксировать публичный observability contract.
2. P0.3 — подготовить и проверить production rollout runbook.
3. После закрытия P0 отдельно согласовать, что берём первым: reliability P1 или
   IPv6 P2.1. По умолчанию приоритет остаётся у P1.

## Исторические планы

Завершённые планы и отчёты перечислены в [`docs/README.md`](README.md). PR #3
не вливается напрямую: его базовые conntrack-задачи вошли в v2.2.0, а
эксплуатационный P0 scope — в v2.3.0. Оставшийся IPv6 перенесён в P2.1. Подробное сопоставление сохранено в
[`PR_3_RECONCILIATION.md`](PR_3_RECONCILIATION.md).
