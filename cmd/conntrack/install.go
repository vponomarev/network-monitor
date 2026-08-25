//go:build linux
// +build linux

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/vponomarev/network-monitor/internal/config"
	"github.com/vponomarev/network-monitor/pkg/embedded"
)

const (
	defaultInstallPath = "/usr/local/bin"
	defaultConfigPath  = "/etc/conntrack/config.yaml"
	defaultSystemdPath = "/etc/systemd/system/conntrack.service"
	defaultRollbackDir = "/var/lib/conntrack/rollback"
)

type rollbackManifest struct {
	BinaryPresent bool `json:"binary_present"`
	UnitPresent   bool `json:"unit_present"`
	WasActive     bool `json:"was_active"`
}

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

var rollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Restore the version saved by the last successful upgrade",
	RunE: func(_ *cobra.Command, _ []string) error {
		if err := restoreRollback(defaultRollbackDir, defaultInstallPath+"/conntrack", defaultSystemdPath); err != nil {
			return fmt.Errorf("rolling back conntrack: %w", err)
		}
		fmt.Println("✓ Rollback complete")
		return nil
	},
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
	sourcePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("getting executable path: %w", err)
	}

	fmt.Println("Installing conntrack...")
	fmt.Printf("Installation path: %s\n", installPath)

	// Проверка запускаемости (проверка прав на запись)
	if err := checkWritePermissions(installPath); err != nil {
		return fmt.Errorf("permission check failed: %w", err)
	}
	if err := validateInstalledConfig(defaultConfigPath); err != nil {
		return err
	}

	wasActive := serviceActive("conntrack")
	refreshBackup := true
	if same, err := filesEqual(sourcePath, binaryPath); err == nil && same {
		if _, err := os.Stat(filepath.Join(defaultRollbackDir, "manifest.json")); err == nil {
			refreshBackup = false
		}
	}
	if refreshBackup {
		if err := createRollback(defaultRollbackDir, binaryPath, defaultSystemdPath, wasActive); err != nil {
			return fmt.Errorf("creating rollback snapshot: %w", err)
		}
	}
	rollbackOnError := func(installErr error) error {
		if err := restoreRollback(defaultRollbackDir, binaryPath, defaultSystemdPath); err != nil {
			return fmt.Errorf("%w; automatic rollback failed: %w", installErr, err)
		}
		return fmt.Errorf("%w; previous installation restored", installErr)
	}

	// 1. Установка бинарника
	if err := installBinaryAtomic(sourcePath, binaryPath); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to install binary: %v\n", err)
		return rollbackOnError(fmt.Errorf("installing binary: %w", err))
	}
	fmt.Printf("✓ Installed binary: %s\n", binaryPath)

	// 2. Install config without overwriting operator changes.
	if _, err := os.Stat(defaultConfigPath); err == nil {
		fmt.Printf("⚠ Config already exists: %s (skipped)\n", defaultConfigPath)
	} else if os.IsNotExist(err) {
		if err := embedded.WriteConfigToFile(defaultConfigPath); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to install config: %v\n", err)
			return rollbackOnError(fmt.Errorf("installing config: %w", err))
		}
		fmt.Printf("✓ Installed config: %s\n", defaultConfigPath)
	} else {
		fmt.Fprintf(os.Stderr, "Failed to check config: %v\n", err)
		return fmt.Errorf("checking config: %w", err)
	}

	// 3. Install systemd unit.
	unitData, err := embedded.GetSystemdUnit()
	if err != nil {
		return rollbackOnError(fmt.Errorf("reading embedded systemd unit: %w", err))
	}
	if err := writeFileAtomic(defaultSystemdPath, unitData, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to install systemd unit: %v\n", err)
		return rollbackOnError(fmt.Errorf("installing systemd unit: %w", err))
	}
	fmt.Printf("✓ Installed systemd unit: %s\n", defaultSystemdPath)

	// 4. Reload systemd.
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return rollbackOnError(fmt.Errorf("reloading systemd: %w", err))
	}
	fmt.Println("✓ Reloaded systemd daemon")

	if wasActive {
		_ = exec.Command("systemctl", "reset-failed", "conntrack").Run()
		if err := exec.Command("systemctl", "restart", "conntrack").Run(); err != nil {
			return rollbackOnError(fmt.Errorf("restarting conntrack: %w", err))
		}
		if err := waitForReadiness(defaultConfigPath, 30*time.Second); err != nil {
			return rollbackOnError(err)
		}
		fmt.Println("✓ Restarted service and verified readiness")
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
	_ = exec.Command("systemctl", "stop", "conntrack").Run()
	_ = exec.Command("systemctl", "disable", "conntrack").Run()
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
	_ = os.RemoveAll(defaultRollbackDir)

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

func installBinaryAtomic(srcPath, destPath string) error {
	source, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer source.Close()
	return writeReaderAtomic(destPath, source, 0755)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	return writeReaderAtomic(path, bytes.NewReader(data), mode)
}

func writeReaderAtomic(path string, source io.Reader, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".conntrack-install-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := io.Copy(tmp, source); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func createRollback(dir, binaryPath, unitPath string, wasActive bool) error {
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(parent, ".rollback-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	manifest := rollbackManifest{WasActive: wasActive}
	if _, err := os.Stat(binaryPath); err == nil {
		manifest.BinaryPresent = true
		if err := copyFile(binaryPath, filepath.Join(tmp, "conntrack"), 0755); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, err := os.Stat(unitPath); err == nil {
		manifest.UnitPresent = true
		if err := copyFile(unitPath, filepath.Join(tmp, "conntrack.service"), 0644); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tmp, "manifest.json"), data, 0600); err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return os.Rename(tmp, dir)
}

func restoreRollback(dir, binaryPath, unitPath string) error {
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return fmt.Errorf("reading rollback manifest: %w", err)
	}
	var manifest rollbackManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parsing rollback manifest: %w", err)
	}
	if manifest.BinaryPresent {
		if err := copyFile(filepath.Join(dir, "conntrack"), binaryPath, 0755); err != nil {
			return fmt.Errorf("restoring binary: %w", err)
		}
	} else if err := os.Remove(binaryPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if manifest.UnitPresent {
		if err := copyFile(filepath.Join(dir, "conntrack.service"), unitPath, 0644); err != nil {
			return fmt.Errorf("restoring systemd unit: %w", err)
		}
	} else if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = exec.Command("systemctl", "stop", "conntrack").Run()
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return err
	}
	_ = exec.Command("systemctl", "reset-failed", "conntrack").Run()
	if manifest.WasActive {
		if err := exec.Command("systemctl", "start", "conntrack").Run(); err != nil {
			return err
		}
		if err := waitForReadiness(defaultConfigPath, 30*time.Second); err != nil {
			return err
		}
	} else {
		_ = exec.Command("systemctl", "stop", "conntrack").Run()
	}
	return os.RemoveAll(dir)
}

func copyFile(sourcePath, destPath string, mode os.FileMode) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	return writeReaderAtomic(destPath, source, mode)
}

func filesEqual(firstPath, secondPath string) (bool, error) {
	first, err := fileSHA256(firstPath)
	if err != nil {
		return false, err
	}
	second, err := fileSHA256(secondPath)
	if err != nil {
		return false, err
	}
	return first == second, nil
}

func fileSHA256(path string) ([sha256.Size]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return [sha256.Size]byte{}, err
	}
	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result, nil
}

func validateInstalledConfig(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if _, err := config.Load(path); err != nil {
		return fmt.Errorf("existing config is incompatible: %w", err)
	}
	return nil
}

func serviceActive(name string) bool {
	return exec.Command("systemctl", "is-active", "--quiet", name).Run() == nil
}

func waitForReadiness(configPath string, timeout time.Duration) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config for readiness: %w", err)
	}
	host := cfg.Global.MetricsAddr
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	url := "http://" + net.JoinHostPort(host, fmt.Sprintf("%d", cfg.Global.MetricsPort)) + "/ready"
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	consecutive := 0
	for time.Now().Before(deadline) {
		if serviceActive("conntrack") {
			response, err := client.Get(url)
			if err == nil {
				_ = response.Body.Close()
				if response.StatusCode == http.StatusOK {
					consecutive++
					if consecutive >= 3 {
						return nil
					}
				} else {
					consecutive = 0
				}
			} else {
				consecutive = 0
			}
		} else {
			consecutive = 0
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("conntrack did not become ready within %s", timeout)
}
