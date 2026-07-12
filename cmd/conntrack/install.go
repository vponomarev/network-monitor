//go:build linux
// +build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/vponomarev/network-monitor/pkg/embedded"
)

const (
	defaultInstallPath = "/usr/local/bin"
	defaultConfigPath  = "/etc/conntrack/config.yaml"
	defaultSystemdPath = "/etc/systemd/system/conntrack.service"
)

// installCmd представляет команду установки
var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install conntrack to system",
	Long:  "Install the conntrack binary, systemd unit and configuration",
	RunE:  runInstall,
}

func init() {
	installCmd.Flags().String("install-path", defaultInstallPath, "Installation path")
}

// deinstallCmd представляет команду удаления
var deinstallCmd = &cobra.Command{
	Use:   "deinstall",
	Short: "Remove conntrack from system",
	Long:  "Remove conntrack binary, eBPF program, systemd unit (config is preserved)",
	RunE:  runDeinstall,
}

// showConfigCmd представляет команду показа конфигурации
var showConfigCmd = &cobra.Command{
	Use:   "show-config",
	Short: "Print sample configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := embedded.GetSampleConfig()
		if err != nil {
			return fmt.Errorf("failed to get sample config: %w", err)
		}
		fmt.Println(string(data))
		return nil
	},
}

func runInstall(cmd *cobra.Command, args []string) error {
	installPath := cmd.Flag("install-path").Value.String()
	if installPath == "" {
		installPath = defaultInstallPath
	}

	binaryPath := filepath.Join(installPath, "conntrack")

	fmt.Println("Installing conntrack...")
	fmt.Printf("Installation path: %s\n", installPath)

	// Проверка запускаемости (проверка прав на запись)
	if err := checkWritePermissions(installPath); err != nil {
		return fmt.Errorf("permission check failed: %w", err)
	}

	// 1. Установка бинарника
	if err := installBinary(binaryPath); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to install binary: %v\n", err)
		return fmt.Errorf("installing binary: %w", err)
	}
	fmt.Printf("✓ Installed binary: %s\n", binaryPath)

	// 2. Install config without overwriting operator changes.
	if _, err := os.Stat(defaultConfigPath); err == nil {
		fmt.Printf("⚠ Config already exists: %s (skipped)\n", defaultConfigPath)
	} else if os.IsNotExist(err) {
		if err := embedded.WriteConfigToFile(defaultConfigPath); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to install config: %v\n", err)
			return fmt.Errorf("installing config: %w", err)
		}
		fmt.Printf("✓ Installed config: %s\n", defaultConfigPath)
	} else {
		fmt.Fprintf(os.Stderr, "Failed to check config: %v\n", err)
		return fmt.Errorf("checking config: %w", err)
	}

	// 3. Install systemd unit.
	if err := embedded.WriteSystemdUnitToFile(defaultSystemdPath); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to install systemd unit: %v\n", err)
		return fmt.Errorf("installing systemd unit: %w", err)
	}
	fmt.Printf("✓ Installed systemd unit: %s\n", defaultSystemdPath)

	// 4. Reload systemd.
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		fmt.Printf("⚠ Failed to reload systemd: %v\n", err)
		fmt.Println("  Run 'sudo systemctl daemon-reload' manually")
	} else {
		fmt.Println("✓ Reloaded systemd daemon")
	}

	fmt.Println()
	fmt.Println("✓ Installation complete!")
	fmt.Println()
	fmt.Println("To start conntrack:")
	fmt.Println("  sudo systemctl enable conntrack")
	fmt.Println("  sudo systemctl start conntrack")
	fmt.Println()
	fmt.Println("To view logs:")
	fmt.Println("  sudo journalctl -u conntrack -f")

	return nil
}

func runDeinstall(cmd *cobra.Command, args []string) error {
	fmt.Println("Deinstalling conntrack...")

	// 1. Остановка сервиса
	exec.Command("systemctl", "stop", "conntrack").Run()
	exec.Command("systemctl", "disable", "conntrack").Run()
	fmt.Println("✓ Stopped and disabled systemd service")

	// 2. Remove managed files (config is preserved).
	files := []struct {
		path string
		name string
	}{
		{defaultInstallPath + "/conntrack", "binary"},
		{defaultSystemdPath, "systemd unit"},
	}

	for _, f := range files {
		if err := os.Remove(f.path); err != nil {
			if os.IsNotExist(err) {
				fmt.Printf("⚠ File not found: %s (skipped)\n", f.path)
			} else {
				fmt.Fprintf(os.Stderr, "Failed to remove %s: %v\n", f.name, err)
			}
		} else {
			fmt.Printf("✓ Removed %s: %s\n", f.name, f.path)
		}
	}

	// Remove the legacy external object from pre-embedded installations.
	_ = os.Remove("/usr/share/conntrack/bpf/conntrack.bpf.o")
	_ = os.Remove("/usr/share/conntrack/bpf")
	_ = os.Remove("/usr/share/conntrack")

	// Reload after removing the unit so systemd forgets it.
	_ = exec.Command("systemctl", "daemon-reload").Run()

	// 3. Config is intentionally preserved.
	fmt.Printf("✓ Preserved config: %s\n", defaultConfigPath)

	fmt.Println()
	fmt.Println("✓ Deinstallation complete!")

	return nil
}

// checkWritePermissions проверяет возможность записи в директорию
func checkWritePermissions(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	testFile := filepath.Join(dir, ".conntrack-install-test")
	if err := os.WriteFile(testFile, []byte(""), 0644); err != nil {
		return fmt.Errorf("cannot write to %s: %w", dir, err)
	}
	os.Remove(testFile)
	return nil
}

// installBinary копирует текущий бинарник в указанное место
func installBinary(destPath string) error {
	// Получаем путь к текущему бинарнику
	srcPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("getting executable path: %w", err)
	}

	// Создаём директорию
	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	// Копируем файл
	srcData, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("reading source binary: %w", err)
	}

	if err := os.WriteFile(destPath, srcData, 0755); err != nil {
		return fmt.Errorf("writing destination: %w", err)
	}

	return nil
}
