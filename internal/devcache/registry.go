// Package devcache registers reusable developer resources and protects them from cleanup.
package devcache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

var ErrProtected = errors.New("registered developer resource is protected")

const schemaVersion = 1

// Resource keeps both the declaration and its physical destination. Registration
// never expires and does not grant permission to remove any other path.
type Resource struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	RepoID       string `json:"repo_id"`
	WorktreeID   string `json:"worktree_id"`
	RepoPath     string `json:"repo_path"`
	Path         string `json:"path"`
	PhysicalPath string `json:"physical_path"`
}

type Registry struct {
	Version   int        `json:"version"`
	Resources []Resource `json:"resources"`
}

var kinds = []string{"pnpm-store", "uv-cache", "go-build", "go-mod", "rust-target", "model"}

func rootFor(env, suffix string) (string, error) {
	if p := os.Getenv(env); p != "" {
		if !filepath.IsAbs(p) {
			return "", fmt.Errorf("%s must be absolute", env)
		}
		return filepath.Clean(p), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, suffix), nil
}

func StateDir() (string, error) { return rootFor("DEV_CACHE_STATE_DIR", ".local/state/dev-cache") }
func DataDir() (string, error)  { return rootFor("DEV_CACHE_DATA_DIR", ".local/share/dev-cache") }

func absolute(p string) (string, error) {
	if p == "" {
		return "", errors.New("empty path")
	}
	if strings.ContainsAny(p, "\x00\r\n") {
		return "", errors.New("path contains a control character")
	}
	p, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.Clean(p), nil
}

// physical resolves existing ancestors too, so a not-yet-created child below a
// symlink is compared to its actual destination rather than its spelling.
func physical(p string) (string, error) {
	p, err := absolute(p)
	if err != nil {
		return "", err
	}
	var tail []string
	for {
		resolved, err := filepath.EvalSymlinks(p)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("resolve %s: %w", p, err)
		}
		// A dangling symlink is unknown, not an ordinary missing directory.
		if info, statErr := os.Lstat(p); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("dangling symlink: %s", p)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "", err
		}
		tail = append(tail, filepath.Base(p))
		p = parent
	}
}

func overlaps(a, b string) bool {
	if runtime.GOOS == "darwin" {
		a = strings.ToLower(a)
		b = strings.ToLower(b)
	}
	rel, err := filepath.Rel(a, b)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}
func related(a, b string) bool { return overlaps(a, b) || overlaps(b, a) }
func digest(s string) string   { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:])[:24] }

func repoIdentity(repo string) (string, string, string, error) {
	abs, err := absolute(repo)
	if err != nil {
		return "", "", "", err
	}
	abs, err = physical(abs)
	if err != nil {
		return "", "", "", err
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", "", "", fmt.Errorf("repository directory unavailable: %s", abs)
	}
	// Git is optional for a plain project, but registered worktrees share the
	// repository identity only when git proves their common directory.
	common := abs
	ctx, cancel := probeContext()
	defer cancel()
	top := exec.CommandContext(ctx, "git", "-C", abs, "rev-parse", "--show-toplevel")
	if out, e := top.Output(); e == nil {
		if p, e := physical(strings.TrimSpace(string(out))); e == nil {
			abs = p
			common = abs
		}
	}
	cmd := exec.CommandContext(ctx, "git", "-C", abs, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if output, e := cmd.Output(); e == nil {
		if p, e := physical(strings.TrimSpace(string(output))); e == nil {
			common = p
		}
	} else if ctx.Err() != nil {
		return "", "", "", errors.New("git repository identity probe timed out")
	}
	return abs, digest(common), digest(abs), nil
}

func validate(r Registry) error {
	if r.Version != schemaVersion {
		return fmt.Errorf("unsupported developer cache registry version %d", r.Version)
	}
	if r.Resources == nil {
		return errors.New("registry resources must be an array")
	}
	seen := map[string]bool{}
	for _, v := range r.Resources {
		if !slices.Contains(kinds, v.Kind) || v.ID == "" || v.RepoID == "" || v.WorktreeID == "" || v.Name == "" {
			return errors.New("invalid developer resource record")
		}
		if seen[v.ID] {
			return errors.New("duplicate developer resource id")
		}
		seen[v.ID] = true
		for _, p := range []string{v.Path, v.PhysicalPath, v.RepoPath} {
			if !filepath.IsAbs(p) || filepath.Clean(p) != p || strings.ContainsAny(p, "\x00\r\n") {
				return errors.New("invalid resource path")
			}
		}
	}
	return nil
}

func readRegistry(state string) (Registry, error) {
	path := filepath.Join(state, "registry.json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if _, stateErr := os.Lstat(state); stateErr == nil {
			return Registry{}, errors.New("developer cache registry is missing; restore registry.json before cleanup")
		} else if !errors.Is(stateErr, os.ErrNotExist) {
			return Registry{}, stateErr
		}
		return Registry{Version: schemaVersion, Resources: []Resource{}}, nil
	}
	if err != nil {
		return Registry{}, fmt.Errorf("read developer cache registry: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 {
		return Registry{}, errors.New("developer cache registry must be a regular file, not writable by others")
	}
	file, err := os.Open(path)
	if err != nil {
		return Registry{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 4<<20))
	decoder.DisallowUnknownFields()
	var r Registry
	if err = decoder.Decode(&r); err != nil {
		return Registry{}, fmt.Errorf("invalid developer cache registry: %w", err)
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		return Registry{}, errors.New("unexpected data after developer cache registry")
	}
	if err = validate(r); err != nil {
		return Registry{}, err
	}
	return r, nil
}

func withRegistryLock(state string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(state), 0700); err != nil {
		return err
	}
	fd, err := unix.Open(state+".lock", unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return fmt.Errorf("open registry lock: %w", err)
	}
	lock := os.NewFile(uintptr(fd), "registry.lock")
	defer lock.Close()
	deadline := time.Now().Add(3 * time.Second)
	for {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return err
		}
		if time.Now().After(deadline) {
			return errors.New("developer cache registry is busy; retry registration")
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer unix.Flock(fd, unix.LOCK_UN) //nolint:errcheck // Closing also releases this advisory lock.
	return fn()
}

func writeRegistry(state string, r Registry) error {
	bytes, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(state, ".registry-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(append(bytes, '\n'))
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(name, filepath.Join(state, "registry.json")); err != nil {
		return err
	}
	directory, err := os.Open(state)
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr = directory.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func Register(repo, kind, path, name string) (Resource, error) {
	if !slices.Contains(kinds, kind) {
		return Resource{}, fmt.Errorf("unsupported resource kind %q", kind)
	}
	if name == "" {
		name = kind
	}
	if strings.ContainsAny(name, "\x00\r\n") {
		return Resource{}, errors.New("invalid resource name")
	}
	repo, repoID, worktreeID, err := repoIdentity(repo)
	if err != nil {
		return Resource{}, err
	}
	state, err := StateDir()
	if err != nil {
		return Resource{}, err
	}
	key := digest(repoID + "\x00" + kind + "\x00" + name)
	if kind == "rust-target" {
		key = digest(repoID + "\x00" + worktreeID + "\x00" + kind + "\x00" + name)
	}
	var result Resource
	err = withRegistryLock(state, func() error {
		r, e := readRegistry(state)
		if e != nil {
			return e
		}
		if _, statErr := os.Lstat(state); errors.Is(statErr, os.ErrNotExist) {
			if e = os.MkdirAll(state, 0700); e != nil {
				return e
			}
			if e = writeRegistry(state, r); e != nil {
				return e
			}
		}

		for _, v := range r.Resources {
			if v.ID == key {
				if path != "" {
					p, e := absolute(path)
					if e != nil {
						return e
					}
					if p != v.Path {
						return errors.New("resource already registered at a different path; migration is not supported")
					}
				}
				current, resolveErr := physical(v.Path)
				if resolveErr != nil {
					return resolveErr
				}
				if current != v.PhysicalPath {
					return errors.New("registered resource physical destination changed; migration is not supported")
				}
				result = v
				return nil
			}
		}
		if kind != "model" && kind != "rust-target" {
			for _, prior := range r.Resources {
				if prior.Kind == kind {
					current, resolveErr := physical(prior.Path)
					if resolveErr != nil {
						return resolveErr
					}
					if current != prior.PhysicalPath {
						return errors.New("registered shared resource physical destination changed; migration is not supported")
					}
					if path == "" {
						path = prior.Path
					} else {
						candidate, e := physical(path)
						if e != nil {
							return e
						}
						if candidate != prior.PhysicalPath {
							return errors.New("shared resource kind already registered at another path; migration is not supported")
						}
					}
					break
				}
			}
		}
		if path == "" {
			if kind == "model" {
				return errors.New("model registration requires --path to an existing model directory")
			}
			root, e := DataDir()
			if e != nil {
				return e
			}
			path = filepath.Join(root, kind)
			if kind == "rust-target" {
				path = filepath.Join(path, repoID, worktreeID)
			}
			if e = os.MkdirAll(path, 0700); e != nil {
				return e
			}
		}
		p, e := absolute(path)
		if e != nil {
			return e
		}
		info, e := os.Stat(p)
		if e != nil {
			return fmt.Errorf("resource directory unavailable: %w", e)
		}
		if !info.IsDir() {
			return errors.New("resource path must be a directory")
		}
		real, e := physical(p)
		if e != nil {
			return e
		}
		if kind == "rust-target" {
			for _, prior := range r.Resources {
				if prior.Kind == kind && prior.WorktreeID != worktreeID && related(real, prior.PhysicalPath) {
					return errors.New("Rust target directories must be isolated between worktrees")
				}
			}
		}
		result = Resource{ID: key, Kind: kind, Name: name, RepoID: repoID, WorktreeID: worktreeID, RepoPath: repo, Path: p, PhysicalPath: real}
		r.Resources = append(r.Resources, result)
		return writeRegistry(state, r)
	})
	return result, err
}

func List(repo string) ([]Resource, error) {
	state, err := StateDir()
	if err != nil {
		return nil, err
	}
	r, err := readRegistry(state)
	if err != nil {
		return nil, err
	}
	if repo == "" {
		return r.Resources, nil
	}
	_, repoID, worktreeID, err := repoIdentity(repo)
	if err != nil {
		return nil, err
	}
	result := []Resource{}
	for _, v := range r.Resources {
		if v.RepoID == repoID && (v.Kind != "rust-target" || v.WorktreeID == worktreeID) {
			result = append(result, v)
		}
	}
	return result, nil
}

// CheckPath refuses registered roots, their descendants, and enclosing parents.
// An absent uninitialized registry opts out; malformed or lost state refuses.
func CheckPath(path string) error {
	state, err := StateDir()
	if err != nil {
		return err
	}
	r, err := readRegistry(state)
	if err != nil {
		return err
	}
	if _, e := os.Lstat(state); errors.Is(e, os.ErrNotExist) {
		return nil
	} else if e != nil {
		return e
	}
	p, err := absolute(path)
	if err != nil {
		return err
	}
	real, err := physical(p)
	if err != nil {
		return err
	}
	protected := []string{state, state + ".lock"}
	if root, e := physical(state); e != nil {
		return e
	} else {
		protected = append(protected, root)
	}
	for _, v := range r.Resources {
		current, e := physical(v.Path)
		if e != nil {
			return e
		}
		protected = append(protected, v.Path, v.PhysicalPath, current)
	}
	for _, root := range protected {
		if related(p, root) || related(real, root) {
			return fmt.Errorf("%w: %s overlaps %s", ErrProtected, p, root)
		}
	}
	return nil
}
