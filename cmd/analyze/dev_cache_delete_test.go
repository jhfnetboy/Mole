//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tw93/mole/internal/devcache"
)

func TestTrashKeepsRegisteredDevCacheAndItsAncestors(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEV_CACHE_STATE_DIR", filepath.Join(root, "state"))
	t.Setenv("DEV_CACHE_DATA_DIR", filepath.Join(root, "shared"))
	repo := filepath.Join(root, "repo")
	cache := filepath.Join(root, "resources", "model")
	for _, path := range []string{repo, cache} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	child := filepath.Join(cache, "weights.bin")
	if err := os.WriteFile(child, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := devcache.Register(repo, "model", cache, "fixture"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{cache, child, filepath.Dir(cache)} {
		if err := validateTrashTarget(path); err == nil {
			t.Fatalf("registered resource overlap must be refused: %s", path)
		}
	}
	for name, sink := range map[string]func(string) error{
		"binary":     moveToTrashViaBinary,
		"filesystem": moveToTrashViaFilesystem,
		"finder":     moveToTrashViaFinder,
	} {
		t.Run(name, func(t *testing.T) {
			if err := sink(cache); err == nil {
				t.Fatal("registered cache must survive each final sink")
			}
			if _, err := os.Stat(child); err != nil {
				t.Fatalf("registered file must survive: %v", err)
			}
		})
	}
	unrelated := filepath.Join(root, "unrelated")
	if err := os.WriteFile(unrelated, []byte("ordinary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateTrashTarget(unrelated); err != nil {
		t.Fatalf("unrelated fixture must remain eligible: %v", err)
	}
}

func TestTrashValidationFailsClosedForUnreadableDevCacheRegistry(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	t.Setenv("DEV_CACHE_STATE_DIR", state)
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "registry.json"), []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, sink := range map[string]func(string) error{
		"validation": validateTrashTarget,
		"binary":     moveToTrashViaBinary,
		"filesystem": moveToTrashViaFilesystem,
		"finder":     moveToTrashViaFinder,
	} {
		t.Run(name, func(t *testing.T) {
			if err := sink(victim); err == nil {
				t.Fatal("unavailable protection must refuse deletion")
			}
			if _, err := os.Stat(victim); err != nil {
				t.Fatalf("victim must survive: %v", err)
			}
		})
	}
}
