//go:build linux
// +build linux

package embedded

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed bpf/conntrack.bpf.o
var ebpfData []byte

//go:embed configs/config.example.yaml
var configData []byte

//go:embed systemd/conntrack.service
var systemdUnitData []byte

// GetEBPFProgram возвращает .o файл как []byte
func GetEBPFProgram() ([]byte, error) {
	if len(ebpfData) == 0 {
		return nil, fmt.Errorf("embedded eBPF program not available (build without embed)")
	}
	return ebpfData, nil
}

// GetSampleConfig возвращает sample config как []byte
func GetSampleConfig() ([]byte, error) {
	if len(configData) == 0 {
		return nil, fmt.Errorf("embedded config not available")
	}
	return configData, nil
}

// GetSystemdUnit возвращает systemd unit файл как []byte
func GetSystemdUnit() ([]byte, error) {
	if len(systemdUnitData) == 0 {
		return nil, fmt.Errorf("embedded systemd unit not available")
	}
	return systemdUnitData, nil
}

// WriteEBPFToFile записывает eBPF программу в файл
func WriteEBPFToFile(path string) error {
	data, err := GetEBPFProgram()
	if err != nil {
		return err
	}

	// Создаём директорию
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
	return len(ebpfData) > 0
}

// ExportEBPFToFile экспортирует embedded .o в указанный файл
func ExportEBPFToFile(path string) error {
	data, err := GetEBPFProgram()
	if err != nil {
		return fmt.Errorf("getting embedded eBPF: %w", err)
	}

	// Создаём директорию
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing eBPF file: %w", err)
	}

	return nil
}
