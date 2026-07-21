package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomicallyReplacesTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "market-data.toml")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := writeFileAtomically(target, []byte("new"), 0o644); err != nil {
		t.Fatalf("writeFileAtomically() error = %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("target = %q, want new", data)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %o, want 644", info.Mode().Perm())
	}
	assertNoTemporaryFiles(t, dir)
}

func TestWriteFileAtomicallyCleansTemporaryFileWhenRenameFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "market-data.toml")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := writeFileAtomically(target, []byte("new"), 0o644); err == nil {
		t.Fatal("writeFileAtomically() error = nil, want rename failure")
	}
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		t.Fatalf("original target changed: info=%v error=%v", info, err)
	}
	assertNoTemporaryFiles(t, dir)
}

func assertNoTemporaryFiles(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".market-data.toml.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files remain: %v", matches)
	}
}
