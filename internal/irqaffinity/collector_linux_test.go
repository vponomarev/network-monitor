//go:build linux

package irqaffinity

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestReadCPUList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "affinity")
	require.NoError(t, os.WriteFile(path, []byte("0-2,8,10-11\n"), 0600))
	cpus, err := readCPUList(path)
	require.NoError(t, err)
	require.Equal(t, []int{0, 1, 2, 8, 10, 11}, cpus)
}

func TestCollectorCorrelatesCrossNUMABusyCPUAndDrops(t *testing.T) {
	root := t.TempDir()
	sysRoot, procRoot := filepath.Join(root, "sys"), filepath.Join(root, "proc")
	writeFixture(t, filepath.Join(sysRoot, "class/net/eth0/device/numa_node"), "0\n")
	writeFixture(t, filepath.Join(sysRoot, "class/net/eth0/device/msi_irqs/100"), "")
	writeFixture(t, filepath.Join(procRoot, "irq/100/effective_affinity_list"), "2\n")
	require.NoError(t, os.MkdirAll(filepath.Join(sysRoot, "devices/system/cpu/cpu2/node1"), 0755))
	for _, kind := range []string{"rx_dropped", "rx_missed_errors", "rx_nohandler"} {
		writeFixture(t, filepath.Join(sysRoot, "class/net/eth0/statistics", kind), "0\n")
	}
	writeFixture(t, filepath.Join(procRoot, "stat"), "cpu 0 0 0 0\ncpu2 100 0 0 100 0 0 0 0\n")
	writeFixture(t, filepath.Join(procRoot, "interrupts"), "           CPU0 CPU1 CPU2\n100: 0 0 10 PCI-MSI eth0\n")

	registry := prometheus.NewRegistry()
	collector := New(Options{SysRoot: sysRoot, ProcRoot: procRoot, BusyThreshold: .8}, zap.NewNop(), registry)
	require.NoError(t, collector.collect())

	writeFixture(t, filepath.Join(procRoot, "stat"), "cpu 0 0 0 0\ncpu2 200 0 0 100 0 0 0 0\n")
	writeFixture(t, filepath.Join(procRoot, "interrupts"), "           CPU0 CPU1 CPU2\n100: 0 0 20 PCI-MSI eth0\n")
	writeFixture(t, filepath.Join(sysRoot, "class/net/eth0/statistics/rx_dropped"), "1\n")
	require.NoError(t, collector.collect())

	require.Equal(t, float64(1), testutil.ToFloat64(collector.crossNUMA.WithLabelValues("eth0", "100")))
	require.Equal(t, float64(1), testutil.ToFloat64(collector.risk.WithLabelValues("eth0")))
	require.Equal(t, float64(1), testutil.ToFloat64(collector.anomaly.WithLabelValues("eth0")))
	require.Greater(t, testutil.ToFloat64(collector.dropRate.WithLabelValues("eth0", "rx_dropped")), float64(0))
}

func TestCollectorTracksAffinityChangesWithoutCountingInventoryChurn(t *testing.T) {
	root := t.TempDir()
	sysRoot, procRoot := filepath.Join(root, "sys"), filepath.Join(root, "proc")
	writeFixture(t, filepath.Join(sysRoot, "class/net/eth0/device/numa_node"), "0\n")
	writeFixture(t, filepath.Join(sysRoot, "class/net/eth0/device/msi_irqs/100"), "")
	for cpu, node := range map[int]int{0: 0, 1: 0, 2: 1} {
		require.NoError(t, os.MkdirAll(filepath.Join(sysRoot, "devices/system/cpu", "cpu"+strconv.Itoa(cpu), "node"+strconv.Itoa(node)), 0755))
	}
	for _, kind := range []string{"rx_dropped", "rx_missed_errors", "rx_nohandler"} {
		writeFixture(t, filepath.Join(sysRoot, "class/net/eth0/statistics", kind), "0\n")
	}
	writeFixture(t, filepath.Join(procRoot, "stat"), "cpu 0 0 0 0\ncpu0 1 0 0 1\ncpu1 1 0 0 1\ncpu2 1 0 0 1\n")
	writeFixture(t, filepath.Join(procRoot, "interrupts"), "100: 1 0 0 PCI-MSI eth0\n")
	writeFixture(t, filepath.Join(procRoot, "irq/100/effective_affinity_list"), "0\n")

	collector := New(Options{SysRoot: sysRoot, ProcRoot: procRoot}, zap.NewNop(), prometheus.NewRegistry())
	require.NoError(t, collector.collect())
	require.Zero(t, testutil.ToFloat64(collector.affinityChanges.WithLabelValues("eth0", "same_numa")))

	writeFixture(t, filepath.Join(procRoot, "irq/100/effective_affinity_list"), "1\n")
	require.NoError(t, collector.collect())
	require.Equal(t, float64(1), testutil.ToFloat64(collector.affinityChanges.WithLabelValues("eth0", "same_numa")))
	require.Zero(t, testutil.ToFloat64(collector.crossNUMATransitions.WithLabelValues("eth0", "enter")))

	writeFixture(t, filepath.Join(procRoot, "irq/100/effective_affinity_list"), "2\n")
	require.NoError(t, collector.collect())
	require.Equal(t, float64(1), testutil.ToFloat64(collector.affinityChanges.WithLabelValues("eth0", "cross_numa")))
	require.Equal(t, float64(1), testutil.ToFloat64(collector.crossNUMATransitions.WithLabelValues("eth0", "enter")))
	require.Greater(t, testutil.ToFloat64(collector.lastAffinityChange.WithLabelValues("eth0", "cross_numa")), float64(0))

	writeFixture(t, filepath.Join(sysRoot, "class/net/eth0/device/msi_irqs/101"), "")
	writeFixture(t, filepath.Join(procRoot, "irq/101/effective_affinity_list"), "0\n")
	require.NoError(t, collector.collect())
	require.Equal(t, float64(1), testutil.ToFloat64(collector.affinityChanges.WithLabelValues("eth0", "same_numa")))
	require.Equal(t, float64(1), testutil.ToFloat64(collector.affinityChanges.WithLabelValues("eth0", "cross_numa")))

	writeFixture(t, filepath.Join(procRoot, "irq/100/effective_affinity_list"), "0\n")
	require.NoError(t, collector.collect())
	require.Equal(t, float64(1), testutil.ToFloat64(collector.crossNUMATransitions.WithLabelValues("eth0", "leave")))
}

func writeFixture(t *testing.T, path, value string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte(value), 0600))
}
