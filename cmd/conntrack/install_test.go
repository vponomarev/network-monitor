//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallBinaryAtomic(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	destination := filepath.Join(dir, "bin", "conntrack")
	if err := os.WriteFile(source, []byte("new-binary"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := installBinaryAtomic(source, destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new-binary" {
		t.Fatalf("installed data = %q", data)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0755 {
		t.Fatalf("installed mode = %o, want 755", info.Mode().Perm())
	}
}

func TestCreateRollbackSnapshot(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "installed", "conntrack")
	unit := filepath.Join(dir, "systemd", "conntrack.service")
	rollback := filepath.Join(dir, "state", "rollback")
	if err := os.MkdirAll(filepath.Dir(binary), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(unit), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("old-binary"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unit, []byte("old-unit"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := createRollback(rollback, binary, unit, true); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"conntrack", "conntrack.service", "manifest.json"} {
		if _, err := os.Stat(filepath.Join(rollback, name)); err != nil {
			t.Fatalf("rollback file %s: %v", name, err)
		}
	}
}

func TestFilesEqual(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	if err := os.WriteFile(first, []byte("same"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("same"), 0600); err != nil {
		t.Fatal(err)
	}
	equal, err := filesEqual(first, second)
	if err != nil || !equal {
		t.Fatalf("filesEqual() = %v, %v", equal, err)
	}
	if err := os.WriteFile(second, []byte("different"), 0600); err != nil {
		t.Fatal(err)
	}
	equal, err = filesEqual(first, second)
	if err != nil || equal {
		t.Fatalf("filesEqual() after change = %v, %v", equal, err)
	}
}
