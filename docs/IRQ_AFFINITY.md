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

## Standalone support report

The release also contains `irqdiag-linux-amd64`. It is read-only, needs no
configuration, and does not change IRQ affinity or network settings. It samples
CPU and IRQ activity for one second and emits a versioned JSON report:

```bash
chmod +x irqdiag-linux-amd64
./irqdiag-linux-amd64 > irq-report.json
# or, with restrictive 0600 file permissions:
./irqdiag-linux-amd64 --output irq-report.json
```

Use `--sample-duration=5s` on a busy host to obtain a less noisy CPU utilization
and interrupt-rate sample. The report includes host name, MAC addresses, kernel
command line, and PCI identifiers; review it before sharing outside the support
channel. Missing sysfs/procfs data is recorded in `warnings` instead of making
the entire collection fail.
