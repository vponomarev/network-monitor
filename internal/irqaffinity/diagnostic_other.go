//go:build !linux

package irqaffinity

import (
	"context"
	"fmt"
)

func diagnose(_ context.Context, _ DiagnosticOptions) (DiagnosticReport, error) {
	return DiagnosticReport{}, fmt.Errorf("IRQ diagnostics are supported on Linux only")
}
