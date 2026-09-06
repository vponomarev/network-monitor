package config

import "testing"

func TestAuditZeroIntervals(t *testing.T) {
	for _, component := range []string{"discovery", "bandwidth", "latency", "dns"} {
		t.Run(component, func(t *testing.T) {
			cfg := DefaultConfig()
			switch component {
			case "discovery":
				cfg.Discovery.Traceroute.Enabled = true
				cfg.Discovery.Traceroute.Interval = "0s"
			case "bandwidth":
				cfg.Bandwidth.Enabled = true
				cfg.Bandwidth.Interfaces = []string{"eth0"}
				cfg.Bandwidth.Interval = "0s"
			case "latency":
				cfg.Latency.Enabled = true
				cfg.Latency.Targets = []string{"127.0.0.1"}
				cfg.Latency.Interval = "0s"
			case "dns":
				cfg.DNS.Enabled = true
				cfg.DNS.Interval = "0s"
			}
			if err := cfg.Validate(); err == nil {
				t.Fatal("zero ticker interval accepted; time.NewTicker will panic")
			}
		})
	}
}
