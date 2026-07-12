# Conntrack productionization — архив решений

Документ фиксирует закрытие проблем, найденных при первоначальном аудите.
Актуальные незавершённые задачи находятся в
[`docs/STATUS_AND_PLAN.md`](../STATUS_AND_PLAN.md).

## Закрыто 2026-07-12

- **C-1:** `bpf_printk` удалён из hot path.
- **C-2:** закрытие/error ring buffer больше не создаёт busy-loop.
- **C-3:** синхронное чтение `/proc/{pid}/comm` удалено из consumer path.
- **C-4:** kernel `ringbuf_full` и userspace `event_channel_full` экспортируются
  через `conntrack_dropped_events_total`.
- **C-6:** production simulation удалена; ошибка eBPF приводит к fail-closed.
- **C-7:** исправлен CO-RE layout; verifier/runtime пройдены на 5.15–6.12.
- **C-8:** устаревшие тесты приведены к текущему API.
- **C-9:** SYN_SENT коррелируется с ESTABLISHED, CLOSE дедуплицируется; tuple и
  PID/comm проверены для обоих направлений.
- **C-10:** standalone binary публикует `/health`, `/ready` и `/metrics`;
  readiness зависит от успешного eBPF attach.

## Перенесено в roadmap

- retention/TTL и ограничение kernel/userspace state — повышенный приоритет;
- IPv6;
- ARM64 после появления runtime-хоста;
- нагрузочная qualification, расширенные тесты, systemd hardening и upgrade.
