package devcache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func fixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	state := filepath.Join(root, "state")
	t.Setenv("DEV_CACHE_STATE_DIR", state)
	t.Setenv("DEV_CACHE_DATA_DIR", filepath.Join(root, "data"))
	repo := filepath.Join(root, "repo")
	if e := os.Mkdir(repo, 0700); e != nil {
		t.Fatal(e)
	}
	return root, repo
}
func register(t *testing.T, repo, kind, path, name string) Resource {
	t.Helper()
	r, e := Register(repo, kind, path, name)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func mkdir(t *testing.T, path string) {
	t.Helper()
	if e := os.MkdirAll(path, 0700); e != nil {
		t.Fatal(e)
	}
}
func git(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid")
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("git %v: %v: %s", args, e, out)
	}
}

func TestRegisterReuseAndKeepExistingPath(t *testing.T) {
	root, repo := fixture(t)
	existing := filepath.Join(root, "existing")
	mkdir(t, existing)
	first := register(t, repo, "go-build", existing, "")
	again := register(t, repo, "go-build", "", "")
	if first != again {
		t.Fatalf("idempotent registration changed: %#v %#v", first, again)
	}
	other := filepath.Join(root, "other")
	mkdir(t, other)
	second := register(t, other, "go-build", "", "")
	if second.Path != existing {
		t.Fatalf("shared cache not reused: %#v", second)
	}
	conflict := filepath.Join(root, "conflict")
	mkdir(t, conflict)
	if _, e := Register(other, "go-build", conflict, "different"); e == nil {
		t.Fatal("conflicting shared store accepted")
	}
	env, e := Environment(repo)
	if e != nil || env["GOCACHE"] != existing {
		t.Fatalf("bad environment: %v %v", env, e)
	}
	rows, e := List(repo)
	if e != nil || len(rows) != 1 {
		t.Fatalf("bad repo listing: %v %v", rows, e)
	}
	if _, e := Register(repo, "go-build", conflict, ""); e == nil {
		t.Fatal("existing registration moved")
	}
}

func TestProtectionCoversPhysicalPathsAndAncestors(t *testing.T) {
	root, repo := fixture(t)
	store := filepath.Join(root, "store")
	mkdir(t, store)
	alias := filepath.Join(root, "alias")
	if e := os.Symlink(store, alias); e != nil {
		t.Fatal(e)
	}
	r := register(t, repo, "model", alias, "my-model")
	for _, p := range []string{store, alias, filepath.Join(store, "new-child"), filepath.Join(alias, "new-child"), root, os.Getenv("DEV_CACHE_STATE_DIR")} {
		if e := CheckPath(p); !errors.Is(e, ErrProtected) {
			t.Errorf("%s not protected: %v", p, e)
		}
	}
	if r.PhysicalPath == r.Path {
		t.Fatal("symlink destination not recorded")
	}
	if e := CheckPath(store + "-sibling"); e != nil {
		t.Fatalf("prefix sibling falsely protected: %v", e)
	}
	changed := filepath.Join(root, "changed")
	mkdir(t, changed)
	if e := os.Remove(alias); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(changed, alias); e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{store, changed} {
		if e := CheckPath(p); !errors.Is(e, ErrProtected) {
			t.Fatalf("old/new physical path %s unprotected: %v", p, e)
		}
	}
}

func TestDarwinCaseAliasProtection(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin conservative casing policy")
	}
	root, repo := fixture(t)
	store := filepath.Join(root, "MixedCase")
	mkdir(t, store)
	register(t, repo, "model", store, "m")
	if e := CheckPath(strings.ToUpper(store)); !errors.Is(e, ErrProtected) {
		t.Fatalf("case alias not protected: %v", e)
	}
}

func TestMissingAndCorruptRegistryFailClosed(t *testing.T) {
	_, repo := fixture(t)
	if e := CheckPath(repo); e != nil {
		t.Fatalf("uninitialized integration should opt out: %v", e)
	}
	register(t, repo, "uv-cache", "", "")
	state := os.Getenv("DEV_CACHE_STATE_DIR")
	path := filepath.Join(state, "registry.json")
	for _, body := range []string{"{", `{"version":99,"resources":[]}`, `{"version":1,"resources":null}`, `{"version":1,"resources":[],"unexpected":true}`, `{"version":1,"resources":[]} garbage`} {
		if e := os.WriteFile(path, []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
		if e := CheckPath(filepath.Join(repo, "unrelated")); e == nil || errors.Is(e, ErrProtected) {
			t.Fatalf("bad state did not fail closed for %s: %v", body, e)
		}
		if _, e := Register(repo, "go-build", "", ""); e == nil {
			t.Fatal("registration overwrote corrupt state")
		}
	}
	if e := os.Remove(path); e != nil {
		t.Fatal(e)
	}
	if e := CheckPath(repo); e == nil {
		t.Fatal("lost registry allowed deletion")
	}
	if _, e := Register(repo, "go-build", "", ""); e == nil {
		t.Fatal("registration silently reinitialized missing registry")
	}
}

func TestDirectoryWithoutRegistryFailsClosed(t *testing.T) {
	_, repo := fixture(t)
	mkdir(t, os.Getenv("DEV_CACHE_STATE_DIR"))
	if e := CheckPath(repo); e == nil {
		t.Fatal("existing state without registry allowed cleanup")
	}
	if _, e := Register(repo, "go-build", "", ""); e == nil {
		t.Fatal("registration silently trusted ambiguous existing state")
	}
}

func TestConcurrentRegistrationKeepsEveryResource(t *testing.T) {
	root, _ := fixture(t)
	const count = 12
	repos := make([]string, count)
	for i := range repos {
		repos[i] = filepath.Join(root, fmt.Sprintf("repo-%d", i))
		mkdir(t, repos[i])
	}
	var wg sync.WaitGroup
	failures := make(chan error, count)
	for _, repo := range repos {
		wg.Go(func() {
			_, e := Register(repo, "uv-cache", "", "")
			if e != nil {
				failures <- e
			}
		})
	}
	wg.Wait()
	close(failures)
	for e := range failures {
		t.Error(e)
	}
	rows, e := List("")
	if e != nil {
		t.Fatal(e)
	}
	if len(rows) != count {
		t.Fatalf("lost registration: got %d want %d", len(rows), count)
	}
	first := rows[0].Path
	for _, r := range rows {
		if r.Path != first {
			t.Fatalf("cache not shared: %v", rows)
		}
	}
}

func TestRustWorktreesAreIsolatedAndSubdirectoriesResolve(t *testing.T) {
	if _, e := exec.LookPath("git"); e != nil {
		t.Skip("git unavailable")
	}
	root, repo := fixture(t)
	git(t, "init", "--initial-branch=main", repo)
	git(t, "-C", repo, "commit", "--allow-empty", "-m", "initial")
	wt := filepath.Join(root, "worktree")
	git(t, "-C", repo, "worktree", "add", "-b", "feature", wt)
	a := register(t, repo, "rust-target", "", "")
	b := register(t, wt, "rust-target", "", "")
	if a.RepoID != b.RepoID || a.WorktreeID == b.WorktreeID || a.Path == b.Path {
		t.Fatalf("worktree identity broken: %#v %#v", a, b)
	}
	sub := filepath.Join(wt, "nested")
	mkdir(t, sub)
	env, e := Environment(sub)
	if e != nil || env["CARGO_TARGET_DIR"] != b.Path {
		t.Fatalf("subdir doesn't find worktree environment: %v %v", env, e)
	}
	if _, e := Register(wt, "rust-target", a.Path, "collision"); e == nil {
		t.Fatal("shared writable target accepted across worktrees")
	}
}

func TestRegistryRoundTripAndModelNames(t *testing.T) {
	root, repo := fixture(t)
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")
	mkdir(t, a)
	mkdir(t, b)
	register(t, repo, "model", a, "one")
	register(t, repo, "model", b, "two")
	env, e := Environment(repo)
	if e != nil || len(env) != 0 {
		t.Fatalf("model generated build configuration: %v %v", env, e)
	}
	bytes, e := os.ReadFile(filepath.Join(os.Getenv("DEV_CACHE_STATE_DIR"), "registry.json"))
	if e != nil {
		t.Fatal(e)
	}
	var registry Registry
	if e = json.Unmarshal(bytes, &registry); e != nil || len(registry.Resources) != 2 {
		t.Fatalf("bad saved registry: %s %v", bytes, e)
	}
	if _, e := Register(repo, "model", "", "no-path"); e == nil {
		t.Fatal("model without existing directory accepted")
	}
}

func TestRunUsesOnlySelectedRepoAndKeepsParentEnvironment(t *testing.T) {
	root, repo := fixture(t)
	r := register(t, repo, "go-build", "", "")
	register(t, repo, "uv-cache", "", "")
	other := filepath.Join(root, "other")
	mkdir(t, other)
	register(t, other, "rust-target", "", "")
	env, e := Environment(other)
	if e != nil || len(env) != 1 || env["CARGO_TARGET_DIR"] == "" {
		t.Fatalf("environment leaked across repositories: %v %v", env, e)
	}
	t.Setenv("GOCACHE", "parent-sentinel")
	output := filepath.Join(root, "env.txt")
	if e := Run(repo, []string{"/bin/sh", "-c", `printf '%s' "$GOCACHE" > "$1"`, "fixture", output}); e != nil {
		t.Fatal(e)
	}
	bytes, e := os.ReadFile(output)
	if e != nil || string(bytes) != r.Path {
		t.Fatalf("run did not set cache: %q %v", bytes, e)
	}
	if os.Getenv("GOCACHE") != "parent-sentinel" {
		t.Fatal("parent environment changed")
	}
	var exitErr *exec.ExitError
	if e := Run(repo, []string{"/bin/sh", "-c", "exit 7"}); !errors.As(e, &exitErr) || exitErr.ExitCode() != 7 {
		t.Fatalf("child error lost: %v", e)
	}
}

func TestDoctorReportsEffectiveToolPathAndUnavailable(t *testing.T) {
	root, repo := fixture(t)
	register(t, repo, "go-build", "", "")
	register(t, repo, "uv-cache", "", "")
	tools := filepath.Join(root, "tools")
	mkdir(t, tools)
	if e := os.WriteFile(filepath.Join(tools, "go"), []byte("#!/bin/sh\nprintf '%s\\n' \"$GOCACHE\"\n"), 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", tools)
	report, e := Doctor(repo)
	if e != nil {
		t.Fatal(e)
	}
	if report.OK {
		t.Fatal("missing uv reported healthy")
	}
	statuses := map[string]string{}
	for _, p := range report.Resources {
		statuses[p.Kind] = p.Status
	}
	if statuses["go-build"] != "verified" || statuses["uv-cache"] != "unavailable" {
		t.Fatalf("unexpected tool statuses: %v", statuses)
	}
}

func TestBuildEnvironmentRejectsRetargetedResource(t *testing.T) {
	root, repo := fixture(t)
	original := filepath.Join(root, "original")
	changed := filepath.Join(root, "changed")
	mkdir(t, original)
	mkdir(t, changed)
	alias := filepath.Join(root, "alias")
	if e := os.Symlink(original, alias); e != nil {
		t.Fatal(e)
	}
	register(t, repo, "uv-cache", alias, "")
	if e := os.Remove(alias); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(changed, alias); e != nil {
		t.Fatal(e)
	}
	if _, e := Environment(repo); e == nil {
		t.Fatal("environment followed an unapproved cache migration")
	}
	if _, e := Register(repo, "uv-cache", "", ""); e == nil {
		t.Fatal("idempotent registration approved path drift")
	}
	for _, p := range []string{original, changed} {
		if e := CheckPath(p); !errors.Is(e, ErrProtected) {
			t.Fatalf("drift protection missing for %s: %v", p, e)
		}
	}
}

func TestPnpmEnvironmentOverridesUppercaseConfig(t *testing.T) {
	t.Setenv("NPM_CONFIG_STORE_DIR", "old-store")
	values := configuredEnvironment(map[string]string{"npm_config_store_dir": "new-store"})
	count := 0
	for _, value := range values {
		if strings.HasPrefix(strings.ToLower(value), "npm_config_store_dir=") {
			count++
			if value != "npm_config_store_dir=new-store" {
				t.Fatalf("old npm config survived: %s", value)
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected one pnpm store override, got %d", count)
	}
}
