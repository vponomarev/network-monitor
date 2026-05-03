//go:build linux
// +build linux

package embedded

import (
	"fmt"
	"os"
	"path/filepath"
)

// Embedded file paths (set during build)
var (
	embeddedEBPFPath   = ""
	embeddedConfigPath = ""
	embeddedSystemdPath = ""
)

// GetEBPFProgram возвращает .o файл как []byte
func GetEBPFProgram() ([]byte, error) {
	// Try to read from embedded path first
	if embeddedEBPFPath != "" {
		return os.ReadFile(embeddedEBPFPath)
	}
	
	// Fallback: try default locations
	defaultPaths := []string{
		"pkg/embedded/bpf/conntrack.bpf.o",
		"/usr/share/conntrack/bpf/conntrack.bpf.o",
	}
	
	for _, path := range defaultPaths {
		if data, err := os.ReadFile(path); err == nil {
			return data, nil
		}
	}
	
	return nil, fmt.Errorf("embedded eBPF program not found")
}

// GetSampleConfig возвращает sample config как []byte
func GetSampleConfig() ([]byte, error) {
	if embeddedConfigPath != "" {
		return os.ReadFile(embeddedConfigPath)
	}
	
	defaultPaths := []string{
		"pkg/embedded/configs/config.example.yaml",
		"/etc/conntrack/config.example.yaml",
	}
	
	for _, path := range defaultPaths {
		if data, err := os.ReadFile(path); err == nil {
			return data, nil
		}
	}
	
	return nil, fmt.Errorf("embedded config not found")
}

// GetSystemdUnit возвращает systemd unit файл как []byte
func GetSystemdUnit() ([]byte, error) {
	if embeddedSystemdPath != "" {
		return os.ReadFile(embeddedSystemdPath)
	}
	
	defaultPaths := []string{
		"pkg/embedded/systemd/conntrack.service",
		"/etc/systemd/system/conntrack.service",
	}
	
	for _, path := range defaultPaths {
		if data, err := os.ReadFile(path); err == nil {
			return data, nil
		}
	}
	
	return nil, fmt.Errorf("embedded systemd unit not found")
}

// WriteEBPFToFile записывает eBPF программу в файл
func WriteEBPFToFile(path string) error {
	data, err := GetEBPFProgram()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	return os.WriteFile(path, data, 0644)
}

// WriteConfigToFile записывает config в файл
func WriteConfigToFile(path string) error {
	data, err := GetSampleConfig()
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// WriteSystemdUnitToFile записывает systemd unit в файл
func WriteSystemdUnitToFile(path string) error {
	data, err := GetSystemdUnit()
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// HasEmbeddedEBPF проверяет, доступна ли embedded версия
func HasEmbeddedEBPF() bool {
	if embeddedEBPFPath != "" {
		_, err := os.Stat(embeddedEBPFPath)
		return err == nil
	}
	
	_, err := os.Stat("pkg/embedded/bpf/conntrack.bpf.o")
	return err == nil
}

// ExportEBPFToFile экспортирует embedded .o в указанный файл
func ExportEBPFToFile(path string) error {
	data, err := GetEBPFProgram()
	if err != nil {
		return fmt.Errorf("getting embedded eBPF: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing eBPF file: %w", err)
	}

	return nil
}
