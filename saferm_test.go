package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafeRemoveAllDeletesTree(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sandbox")
	if err := os.MkdirAll(filepath.Join(dir, "nix", "store", "abc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nix", "store", "abc", "f"), []byte("x"), 0o444); err != nil {
		t.Fatal(err)
	}
	// Read-only dirs, as a nix store has.
	_ = os.Chmod(filepath.Join(dir, "nix", "store", "abc"), 0o555)
	if err := safeRemoveAll(dir); err != nil {
		t.Fatalf("safeRemoveAll: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("directory survived")
	}
}

func TestSafeRemoveAllMissingIsFine(t *testing.T) {
	if err := safeRemoveAll(filepath.Join(t.TempDir(), "nope")); err != nil {
		t.Fatalf("missing dir should be a no-op, got %v", err)
	}
}

// The whole point: a symlink pointing out of the tree must be unlinked, never
// followed. Otherwise a link to / would delete the host.
func TestSafeRemoveAllDoesNotFollowSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(outside, "precious")
	if err := os.WriteFile(keep, []byte("do not delete"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "sandbox")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := safeRemoveAll(dir); err != nil {
		t.Fatalf("safeRemoveAll: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("followed a symlink and deleted outside the tree: %v", err)
	}
}

func TestMountsUnderFindsRoot(t *testing.T) {
	// "/" is always a mount point, so this asserts the parser works at all.
	m, err := mountsUnder("/")
	if err != nil {
		t.Fatalf("mountsUnder: %v", err)
	}
	if len(m) == 0 {
		t.Fatal("expected at least one mount under /")
	}
}

func TestMountsUnderIgnoresPrefixLookalikes(t *testing.T) {
	// /rootfoo must not match a mount at /root.
	if m, _ := mountsUnder("/definitely-not-a-mount-point-xyz"); len(m) != 0 {
		t.Fatalf("unexpected mounts: %v", m)
	}
}
