//go:build !linux
// +build !linux

package main

import (
	"fmt"
	"os"
)

// checkPlatform returns an error on non-Linux platforms
func checkPlatform() error {
	return fmt.Errorf("conntrack is only available on Linux. Current OS: %s", os.Getenv("GOOS"))
}
