package irqaffinity

import (
	"context"
	"time"
)

const DiagnosticSchemaVersion = 1

type DiagnosticOptions struct {
	SysRoot        string
	ProcRoot       string
	EtcRoot        string
	SampleDuration time.Duration
}

type DiagnosticReport struct {
	SchemaVersion int                   `json:"schema_version"`
	ToolVersion   string                `json:"tool_version,omitempty"`
	GitCommit     string                `json:"git_commit,omitempty"`
	BuildTime     string                `json:"build_time,omitempty"`
	CollectedAt   time.Time             `json:"collected_at"`
	Hostname      string                `json:"hostname,omitempty"`
	KernelRelease string                `json:"kernel_release,omitempty"`
	Architecture  string                `json:"architecture"`
	OSRelease     map[string]string     `json:"os_release,omitempty"`
	KernelCmdline string                `json:"kernel_cmdline,omitempty"`
	CPU           CPUDiagnostic         `json:"cpu"`
	Interfaces    []InterfaceDiagnostic `json:"interfaces"`
	Softnet       []SoftnetDiagnostic   `json:"softnet"`
	Warnings      []string              `json:"warnings,omitempty"`
}

type CPUDiagnostic struct {
	OnlineCPUs         []int                `json:"online_cpus,omitempty"`
	IsolatedCPUs       []int                `json:"isolated_cpus,omitempty"`
	NoHzFullCPUs       []int                `json:"nohz_full_cpus,omitempty"`
	DefaultIRQAffinity []int                `json:"default_irq_affinity,omitempty"`
	LogicalCPUs        []LogicalCPU         `json:"logical_cpus,omitempty"`
	NUMANodes          []NUMANodeDiagnostic `json:"numa_nodes,omitempty"`
}

type LogicalCPU struct {
	ID               int     `json:"id"`
	NUMANode         int     `json:"numa_node"`
	CoreID           int     `json:"core_id"`
	PhysicalPackage  int     `json:"physical_package_id"`
	UtilizationRatio float64 `json:"utilization_ratio"`
}

type NUMANodeDiagnostic struct {
	ID   int   `json:"id"`
	CPUs []int `json:"cpus,omitempty"`
}

type InterfaceDiagnostic struct {
	Name       string            `json:"name"`
	MACAddress string            `json:"mac_address,omitempty"`
	OperState  string            `json:"oper_state,omitempty"`
	Carrier    string            `json:"carrier,omitempty"`
	MTU        int               `json:"mtu,omitempty"`
	SpeedMbps  int               `json:"speed_mbps,omitempty"`
	Duplex     string            `json:"duplex,omitempty"`
	PCIAddress string            `json:"pci_address,omitempty"`
	Driver     string            `json:"driver,omitempty"`
	PCIVendor  string            `json:"pci_vendor,omitempty"`
	PCIDevice  string            `json:"pci_device,omitempty"`
	PCIClass   string            `json:"pci_class,omitempty"`
	NUMANode   int               `json:"numa_node"`
	LocalCPUs  []int             `json:"local_cpus,omitempty"`
	Queues     []QueueDiagnostic `json:"queues,omitempty"`
	Statistics map[string]uint64 `json:"statistics,omitempty"`
	IRQs       []IRQDiagnostic   `json:"irqs,omitempty"`
	Warnings   []string          `json:"warnings,omitempty"`
}

type QueueDiagnostic struct {
	Name         string `json:"name"`
	RPSCPUs      []int  `json:"rps_cpus,omitempty"`
	RPSFlowCount uint64 `json:"rps_flow_count,omitempty"`
	XPSCPUs      []int  `json:"xps_cpus,omitempty"`
}

type IRQDiagnostic struct {
	IRQ                 int               `json:"irq"`
	Description         string            `json:"description,omitempty"`
	CountsByCPU         map[string]uint64 `json:"counts_by_cpu,omitempty"`
	TotalInterrupts     uint64            `json:"total_interrupts"`
	InterruptsPerSecond float64           `json:"interrupts_per_second"`
	EffectiveAffinity   []int             `json:"effective_affinity,omitempty"`
	ConfiguredAffinity  []int             `json:"configured_affinity,omitempty"`
	AffinityHint        []int             `json:"affinity_hint,omitempty"`
	TargetNUMANodes     []int             `json:"target_numa_nodes,omitempty"`
	CrossNUMA           bool              `json:"cross_numa"`
}

type SoftnetDiagnostic struct {
	CPU            int    `json:"cpu"`
	Processed      uint64 `json:"processed"`
	Dropped        uint64 `json:"dropped"`
	TimeSqueeze    uint64 `json:"time_squeeze"`
	CPUCollision   uint64 `json:"cpu_collision"`
	ReceivedRPS    uint64 `json:"received_rps"`
	FlowLimitCount uint64 `json:"flow_limit_count"`
}

func Diagnose(ctx context.Context, options DiagnosticOptions) (DiagnosticReport, error) {
	return diagnose(ctx, options)
}
