//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAvailableOldPathRemovesStaleBackup(t *testing.T) {
	dir := t.TempDir()
	appPath := filepath.Join(dir, "PowerPilot.exe")
	oldPath := appPath + ".old"
	if err := os.WriteFile(oldPath, []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}

	got, err := availableOldPath(appPath)
	if err != nil {
		t.Fatal(err)
	}
	if got != oldPath {
		t.Fatalf("got %q, want %q", got, oldPath)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("stale backup was not removed: %v", err)
	}
}

func TestAvailableOldPathFallsBackWhenPreferredCannotBeRemoved(t *testing.T) {
	dir := t.TempDir()
	appPath := filepath.Join(dir, "PowerPilot.exe")
	oldPath := appPath + ".old"
	if err := os.Mkdir(oldPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldPath, "locked"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}

	got, err := availableOldPath(appPath)
	if err != nil {
		t.Fatal(err)
	}
	want := appPath + ".old.1"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
