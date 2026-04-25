//go:build !linux
// +build !linux

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintf(os.Stderr, "Error: pktloss is only available on Linux. Current OS: %s\n", getGOOS())
	os.Exit(1)
}

// checkPlatform returns an error on non-Linux platforms
func checkPlatform() error {
	return fmt.Errorf("pktloss is only available on Linux. Current OS: %s", getGOOS())
}

func getGOOS() string {
	if os.Getenv("GOOS") != "" {
		return os.Getenv("GOOS")
	}
	return "unknown (non-Linux)"
}
