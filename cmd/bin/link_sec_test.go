package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedactStrong(t *testing.T) {
	in := "DB_PASSWORD=hunter2hunter2 token ghp_" + strings.Repeat("a", 36) + " Authorization: Bearer abcdefghijklmnopqrstuvwxyz0123"
	out, ch := linkRedact(in)
	if !ch || strings.Contains(out, "hunter2") || strings.Contains(out, "ghp_") || strings.Contains(out, "abcdefghijkl") {
		t.Fatalf("not redacted: %q", out)
	}
}

func TestDeniedMore(t *testing.T) {
	for _, n := range []string{"a/terraform.tfstate", ".bash_history", "x/prod.tfvars", ".terraform/p.json", "k/id_rsa", "db.sqlite3"} {
		if !linkIsDenied(n) {
			t.Errorf("%s should be denied", n)
		}
	}
}

func TestDepsSymlinkEscape(t *testing.T) {
	out := t.TempDir()
	os.WriteFile(filepath.Join(out, "secret.json"), []byte(`{"dependencies":{"leak":"1"}}`), 0o600)
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(out, "secret.json"), filepath.Join(dir, "package.json")); err != nil {
		t.Skip("symlink unsupported")
	}
	real, _ := filepath.EvalSymlinks(dir)
	r := &linkRoot{dir: real}
	res, _ := r.opDeps(linkArgs{})
	if m, ok := res.(map[string]any); ok && len(m["sources"].([]string)) > 0 {
		t.Fatal("symlink escape")
	}
}
