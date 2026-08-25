package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/vponomarev/network-monitor/internal/irqaffinity"
)

var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "irqdiag:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("irqdiag", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	output := flags.String("output", "", "write JSON report to this file instead of stdout")
	sampleDuration := flags.Duration("sample-duration", time.Second, "interval used for CPU utilization and IRQ rate")
	compact := flags.Bool("compact", false, "emit compact JSON")
	showVersion := flags.Bool("version", false, "print version and exit")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	if *showVersion {
		_, err := fmt.Fprintf(stdout, "%s commit=%s built=%s\n", Version, GitCommit, BuildTime)
		return err
	}
	if *sampleDuration < 0 {
		return fmt.Errorf("sample-duration must not be negative")
	}

	report, err := irqaffinity.Diagnose(ctx, irqaffinity.DiagnosticOptions{SampleDuration: *sampleDuration})
	if err != nil {
		return fmt.Errorf("collecting diagnostics: %w", err)
	}
	report.ToolVersion = Version
	report.GitCommit = GitCommit
	report.BuildTime = BuildTime

	writer := stdout
	var file *os.File
	if *output != "" {
		file, err = os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			return fmt.Errorf("opening output file: %w", err)
		}
		writer = file
	}
	encoder := json.NewEncoder(writer)
	if !*compact {
		encoder.SetIndent("", "  ")
	}
	encodeErr := encoder.Encode(report)
	if file != nil {
		closeErr := file.Close()
		if encodeErr == nil {
			encodeErr = closeErr
		}
	}
	if encodeErr != nil {
		return fmt.Errorf("writing JSON report: %w", encodeErr)
	}
	return nil
}
