# Network Monitor — статус и план развития

*Актуализировано: 2026-07-13*

## Поддерживаемый scope

| Компонент | Назначение | Статус |
|---|---|---|
| `netmon` / `tcploss` | Мониторинг TCP-ретрансмитов | Production-ready |
| standalone `conntrack` | Трекинг жизненного цикла TCP-соединений | Release candidate |
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

## Текущий релизный scope

1. [x] Убрать ARM из GitHub Release и документации поставки.
2. [x] Прогнать новый E2E-скрипт одним `conntrack-linux-amd64` артефактом на
   четырёх поддерживаемых ядрах — PASS на 5.15, 6.1, 6.8 и 6.12.
3. [ ] Собрать release-кандидат на Linux-хосте, проверить установку из bundle,
   readiness, метрики, трафик, restart и deinstall.
4. [ ] После успешной qualification выпустить новую minor-версию.

Alerts относятся к внешнему контуру мониторинга и не блокируют этот релиз.

## Roadmap после первого production-релиза conntrack

### Повышенный приоритет

- retention/TTL и жёсткие лимиты для незакрытых соединений, потерянных `CLOSE`,
  kernel correlation maps и userspace state; отдельные метрики очистки;
- безопасный upgrade и rollback: сохранение config, проверка совместимости,
  повторная установка и восстановление после неуспешного обновления;
- продолжительный soak/load-профиль с пределами CPU, RSS и допустимой долей drops.

### Плановое развитие

- расширенные lifecycle/API/concurrency/error-path тесты conntrack;
- усиление systemd sandbox и переход с `CAP_SYS_ADMIN` на минимальные capability
  там, где это совместимо с поддерживаемыми ядрами;
- IPv6 для conntrack и TCP-loss;
- ARM64: отдельный `vmlinux.h`, архитектурная eBPF-сборка и реальный runtime-хост;
- container deployment guide, Compose и Kubernetes/Helm;
- обновление GitHub Actions, использующих устаревающий Node.js 20;
- версионирование схемы метрик, dashboards и внешних alert rules.

Нагрузочные испытания, расширенные тесты, sandbox hardening, upgrade/rollback,
IPv6, ARM64 и orchestration сознательно отложены и не входят в текущий scope.
