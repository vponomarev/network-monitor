//go:build linux
// +build linux

// Simple test to verify eBPF .o file loads correctly
// Usage: sudo go run verify_elf.go /path/to/conntrack.bpf.o

package main

import (
	"fmt"
	"os"
	"syscall"

	"github.com/cilium/ebpf"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run verify_elf.go <path_to_elf>")
		os.Exit(1)
	}

	elfPath := os.Args[1]
	fmt.Printf("Loading eBPF ELF: %s\n", elfPath)

	// Load collection spec
	spec, err := ebpf.LoadCollectionSpec(elfPath)
	if err != nil {
		fmt.Printf("ERROR loading collection spec: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Collection spec loaded successfully\n")
	fmt.Printf("Programs: %v\n", getMapKeys(spec.Programs))
	fmt.Printf("Maps: %v\n", getMapKeys(spec.Maps))

	// Try to load the collection (this requires CAP_BPF)
	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		fmt.Printf("ERROR creating collection: %v\n", err)
		os.Exit(1)
	}
	defer coll.Close()

	fmt.Printf("Collection loaded successfully\n")
	fmt.Printf("SUCCESS: eBPF ELF is valid and loads correctly\n")
}

func getMapKeys[K comparable, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// Check if running as root
func init() {
	if syscall.Geteuid() != 0 {
		fmt.Println("WARNING: Not running as root. Some operations may fail.")
	}
}
