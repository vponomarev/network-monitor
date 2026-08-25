# IRQ/NUMA affinity diagnostics

`netmon` can detect a receive-loss pattern caused by active NIC MSI-X queues
being mapped to busy CPUs on a different NUMA node from the PCI device.

The collector combines NIC NUMA placement, MSI-X IRQ affinity, per-CPU load,
IRQ activity, and receive-drop counters from Linux sysfs/procfs.

```yaml
irq_affinity:
  enabled: true
  interval: 15s
  busy_threshold: 0.80
```

Principal metrics:

- `netmon_irq_affinity_cross_numa{interface,irq}` — the affinity contains CPUs
  outside the NIC NUMA node;
- `netmon_irq_affinity_target_cpu_utilization_ratio{interface,irq}` — maximum
  utilization in the IRQ CPU mask;
- `netmon_irq_affinity_interrupts_per_second{interface,irq}` — queue activity;
- `netmon_irq_affinity_risk{interface}` — an active IRQ targets a busy remote
  NUMA CPU;
- `netmon_irq_affinity_packet_loss_anomaly{interface}` — that risk coincides
  with increasing RX dropped/missed/nohandler counters;
- `netmon_irq_affinity_monitored_interfaces` and
  `netmon_irq_affinity_collector_up` expose missing platform data explicitly.

Suggested alert:

```promql
max_over_time(netmon_irq_affinity_packet_loss_anomaly[5m]) > 0
```

The anomaly is diagnostic evidence, not proof of causality. Driver-specific
ring exhaustion counters may provide additional confirmation; the portable
collector deliberately relies only on kernel sysfs/procfs interfaces.
