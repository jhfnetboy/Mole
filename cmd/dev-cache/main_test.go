package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCLIRegisterEnvironmentAndGuard(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if e := os.Mkdir(repo, 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("DEV_CACHE_STATE_DIR", filepath.Join(root, "state"))
	t.Setenv("DEV_CACHE_DATA_DIR", filepath.Join(root, "data"))
	var out, stderr bytes.Buffer
	if status := run([]string{"register", "--repo", repo, "--kind", "go-build"}, &out, &stderr); status != 0 {
		t.Fatalf("register: %d %s", status, stderr.String())
	}
	out.Reset()
	stderr.Reset()
	if status := run([]string{"env", "--repo", repo}, &out, &stderr); status != 0 {
		t.Fatalf("env: %d %s", status, stderr.String())
	}
	var env map[string]string
	if e := json.Unmarshal(out.Bytes(), &env); e != nil {
		t.Fatal(e)
	}
	path := env["GOCACHE"]
	if path == "" {
		t.Fatal("missing GOCACHE")
	}
	out.Reset()
	stderr.Reset()
	if status := run([]string{"guard", "--path", path}, &out, &stderr); status != 3 {
		t.Fatalf("guard: %d %s %s", status, out.String(), stderr.String())
	}
	out.Reset()
	stderr.Reset()
	if status := run([]string{"guard", "--path", filepath.Join(root, "unrelated")}, &out, &stderr); status != 0 {
		t.Fatalf("unrelated guard: %d %s", status, stderr.String())
	}
	if e := os.WriteFile(filepath.Join(root, "state", "registry.json"), []byte("invalid"), 0600); e != nil {
		t.Fatal(e)
	}
	out.Reset()
	stderr.Reset()
	if status := run([]string{"guard", "--path", path}, &out, &stderr); status != 2 {
		t.Fatalf("corrupt guard: %d %s", status, stderr.String())
	}
}

func TestCLIRejectsMissingAndUnexpectedArguments(t *testing.T) {
	for _, args := range [][]string{{"env"}, {"guard"}, {"unknown"}, {"list", "unexpected"}, {"register", "--repo", "x", "--invalid"}} {
		var out, stderr bytes.Buffer
		if status := run(args, &out, &stderr); status != 2 {
			t.Errorf("%v: got %d", args, status)
		}
	}
}
