//go:build linux

package irqaffinity

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDiagnoseReportsCrossNUMAIRQAndSoftnetDrops(t *testing.T) {
	root := t.TempDir()
	sysRoot, procRoot, etcRoot := filepath.Join(root, "sys"), filepath.Join(root, "proc"), filepath.Join(root, "etc")
	writeFixture(t, filepath.Join(sysRoot, "devices/system/cpu/online"), "0-1\n")
	writeFixture(t, filepath.Join(sysRoot, "devices/system/cpu/cpu0/node0/cpulist"), "0\n")
	writeFixture(t, filepath.Join(sysRoot, "devices/system/cpu/cpu1/node1/cpulist"), "1\n")
	writeFixture(t, filepath.Join(sysRoot, "devices/system/cpu/cpu0/topology/core_id"), "0\n")
	writeFixture(t, filepath.Join(sysRoot, "devices/system/cpu/cpu0/topology/physical_package_id"), "0\n")
	writeFixture(t, filepath.Join(sysRoot, "devices/system/cpu/cpu1/topology/core_id"), "1\n")
	writeFixture(t, filepath.Join(sysRoot, "devices/system/cpu/cpu1/topology/physical_package_id"), "1\n")
	writeFixture(t, filepath.Join(sysRoot, "devices/system/node/node0/cpulist"), "0\n")
	writeFixture(t, filepath.Join(sysRoot, "devices/system/node/node1/cpulist"), "1\n")
	writeFixture(t, filepath.Join(sysRoot, "class/net/eth0/device/numa_node"), "0\n")
	writeFixture(t, filepath.Join(sysRoot, "class/net/eth0/device/msi_irqs/100"), "")
	writeFixture(t, filepath.Join(sysRoot, "class/net/eth0/statistics/rx_dropped"), "7\n")
	writeFixture(t, filepath.Join(procRoot, "irq/100/effective_affinity_list"), "1\n")
	writeFixture(t, filepath.Join(procRoot, "irq/100/smp_affinity_list"), "1\n")
	writeFixture(t, filepath.Join(procRoot, "irq/default_smp_affinity"), "00000003\n")
	writeFixture(t, filepath.Join(procRoot, "stat"), "cpu 1 0 0 1\ncpu0 1 0 0 1\ncpu1 1 0 0 1\n")
	writeFixture(t, filepath.Join(procRoot, "interrupts"), "CPU0 CPU1\n100: 0 42 PCI-MSI eth0-rx\n")
	writeFixture(t, filepath.Join(procRoot, "net/softnet_stat"), "00000001 00000002 00000003 0 0 0 0 0 00000004 00000005 00000006\n")
	writeFixture(t, filepath.Join(etcRoot, "os-release"), "ID=test\n")

	report, err := Diagnose(context.Background(), DiagnosticOptions{SysRoot: sysRoot, ProcRoot: procRoot, EtcRoot: etcRoot, SampleDuration: -time.Nanosecond})
	require.NoError(t, err)
	require.Len(t, report.Interfaces, 1)
	require.Equal(t, []int{0, 1}, report.CPU.DefaultIRQAffinity)
	require.Equal(t, uint64(7), report.Interfaces[0].Statistics["rx_dropped"])
	require.Len(t, report.Interfaces[0].IRQs, 1)
	require.True(t, report.Interfaces[0].IRQs[0].CrossNUMA)
	require.Equal(t, []int{1}, report.Interfaces[0].IRQs[0].TargetNUMANodes)
	require.Len(t, report.Softnet, 1)
	require.Equal(t, uint64(2), report.Softnet[0].Dropped)
}

func TestReadCPUMask(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mask")
	writeFixture(t, path, "00000001,00000003\n")
	cpus, err := readCPUMask(path)
	require.NoError(t, err)
	require.Equal(t, []int{0, 1, 32}, cpus)
}
