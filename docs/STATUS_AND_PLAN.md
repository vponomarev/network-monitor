# Network Monitor — статус и план развития

*Актуализировано: 2026-07-17*

## Поддерживаемый scope

| Компонент | Назначение | Статус |
|---|---|---|
| `netmon` / `tcploss` | Мониторинг TCP-ретрансмитов | Production-ready |
| standalone `conntrack` | Трекинг жизненного цикла TCP-соединений | Production-qualified в v2.2.0 |
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

## Текущий план работ

Работы выполняются последовательно. Новый функциональный scope не добавляется,
пока не закрыты эксплуатационные риски P0.

### P0.1 — retention и ограничение состояния (реализовано, ожидает релиза)

- [x] TTL 24 часа для незакрытых соединений и записей после потерянного `CLOSE`;
- [x] настраиваемые жёсткие лимиты kernel correlation maps и userspace state;
- [x] периодическая очистка без goroutine на каждое закрытое соединение;
- [x] метрики текущего размера, cleanup, eviction и overflow;
- [x] unit/race/verifier и E2E одним бинарником на ядрах 5.15, 6.1, 6.8 и 6.12.

Defaults: `state_ttl: 24h`, `cleanup_interval: 1m`, не более 10240 tracked и
16384 pending kernel entries. Реализация находится в рабочей ветке и ещё не
выпущена как новая версия.

### P0.2 — эксплуатационная квалификация (завершена)

- [x] автоматизированный soak-профиль с пределами CPU, RSS, drops и размера состояния;
- [x] короткая проверка профиля на ядре 6.12: 20 секунд, RSS 52720 KiB,
  CPU 9.7%, 21 state entry, без новых drops;
- [x] 30-минутный прогон на ядре 6.12: RSS 55576 KiB, CPU 10.6%,
  максимум 433 aggregate state entries, без новых drops;
- [x] детерминированные тесты вытеснения при заполнении userspace-лимита и
  TTL-очистки userspace/kernel state при длительном отсутствии `CLOSE`.

### P0.3 — безопасный upgrade и rollback (реализовано, ожидает релиза)

- [x] сохранение существующей конфигурации и проверка её совместимости;
- [x] атомарная установка и повторная установка поверх работающей версии;
- [x] автоматическое восстановление сервиса после неуспешного обновления;
- [x] явная команда `conntrack rollback` с проверяемым rollback snapshot;
- [x] изолированный E2E для upgrade, reinstall, explicit и automatic rollback.

## Последующее развитие

### P1

- расширенные lifecycle/API/concurrency/error-path тесты conntrack;
- усиление systemd sandbox и переход с `CAP_SYS_ADMIN` на минимальные capability
  там, где это совместимо с поддерживаемыми ядрами;
- IPv6 для conntrack и TCP-loss;
- ARM64: отдельный `vmlinux.h`, архитектурная eBPF-сборка и реальный runtime-хост;
- container deployment guide, Compose и Kubernetes/Helm;
- обновление GitHub Actions, использующих устаревающий Node.js 20;
- версионирование схемы метрик, dashboards и внешних alert rules.

### P2

- объединение `netmon` и `conntrack` в единый управляемый daemon;
- дополнительные модули bandwidth, latency, DNS и topology;
- контейнерная orchestration после стабилизации standalone lifecycle.

Расширенные тесты, sandbox hardening, IPv6, ARM64 и orchestration сознательно
отложены. Alerts относятся к внешнему контуру мониторинга и остаются вне scope.

## Исторические планы

Завершённые планы и отчёты перечислены в [`docs/README.md`](README.md). PR #3
не вливается напрямую: его реализованные conntrack-задачи уже вошли в v2.2.0,
а оставшийся IPv6 перенесён в P1. Подробное сопоставление сохранено в
[`PR_3_RECONCILIATION.md`](PR_3_RECONCILIATION.md).
