# APPENDIX — Побочные находки Фазы 2 и что снова откладывается

Фиксируем здесь то, что всплывёт по ходу Фазы 2, но не входит в её задачи, чтобы
не потерять (по образцу APPENDIX Фазы 1).

---

## Известные смежные риски (проверить при работе, вынести в задачи при подтверждении)

- **CO-RE по `struct sock` в kprobe-ветках conntrack.** `kprobe/tcp_connect`,
  `kretprobe/inet_csk_accept`, `kprobe/tcp_close` читают поля через
  `BPF_CORE_READ(sk, __sk_common...)`. `struct sock_common` в BTF ядра есть, так
  что это обычно релоцируется. Но после фикса `bpf/vmlinux.h` (TASK-01/02) —
  проверить, что урезанные определения `struct sock`/`sock_common` в заголовке
  совместимы с BTF на 6.17. Если всплывёт релокация — отдельная задача.

- **arm64.** Всё тестируется на x86_64. eBPF под arm64 (сборка `-D__TARGET_ARCH_arm64`,
  загрузка на arm64-ядре) в этой фазе не проверяется. Отдельный трек при необходимости.

- **Дашборды Grafana для conntrack.** После стабилизации метрик (TASK-06/09) может
  понадобиться дашборд/алерты в `dashboards/` — вне текущих задач.

- **Docker/релиз для conntrack.** В Фазе 1 Docker и conntrack исключены из релиза.
  Когда conntrack станет прод-готовым, можно вернуть публикацию conntrack-образа
  и/или бинаря в `release.yml` (сейчас релиз только netmon). Отдельная задача.

## Перенос из Фазы 1

Пункты `C-1…C-7` из
[`../completed/prod-readiness/APPENDIX-conntrack-later.md`](../completed/prod-readiness/APPENDIX-conntrack-later.md)
раскрыты в задачи Фазы 2:

| C-номер | Задача Фазы 2 |
|---------|---------------|
| C-1 (bpf_printk) | TASK-03 |
| C-2 (ringbuf busy-loop) | TASK-04 |
| C-3 (sync /proc comm) | TASK-05 |
| C-4 (dropped metric) | TASK-06 |
| C-5 (IPv6) | TASK-10 |
| C-6 (симуляция в проде) | TASK-08 |
| C-7 (CO-RE conntrack) | TASK-02 (+ TASK-01 для общего заголовка) |
| C-8 (устаревший тест) | уже исправлен в Фазе 1; тесты расширяются в TASK-13 |

Дополнительно (нет C-номера, выявлено при планировании Фазы 2):
- Проблема переносимости **tcploss** на 6.17 → TASK-01.
- Отсутствие HTTP-сервера у standalone conntrack → TASK-07.
- Отсутствие разметки по ролям/локациям у conntrack → TASK-09.
