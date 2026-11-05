package devcache

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

func probeContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func variable(kind string) string {
	switch kind {
	case "pnpm-store":
		return "npm_config_store_dir"
	case "uv-cache":
		return "UV_CACHE_DIR"
	case "go-build":
		return "GOCACHE"
	case "go-mod":
		return "GOMODCACHE"
	case "rust-target":
		return "CARGO_TARGET_DIR"
	default:
		return ""
	}
}

func Environment(repo string) (map[string]string, error) {
	resources, err := List(repo)
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, r := range resources {
		key := variable(r.Kind)
		if key == "" {
			continue
		}
		if prior, ok := result[key]; ok && prior != r.Path {
			return nil, fmt.Errorf("multiple %s resources resolve to different paths", r.Kind)
		}
		current, resolveErr := physical(r.Path)
		if resolveErr != nil {
			return nil, resolveErr
		}
		if current != r.PhysicalPath {
			return nil, fmt.Errorf("registered resource path changed physical destination: %s; reconcile before building", r.Path)
		}
		result[key] = r.Path
	}
	return result, nil
}

func configuredEnvironment(values map[string]string) []string {
	env := os.Environ()
	for key, value := range values {
		prefix := key + "="
		envFiltered := env[:0]
		for _, item := range env {
			matches := strings.HasPrefix(item, prefix)
			if key == "npm_config_store_dir" {
				if index := strings.IndexByte(item, '='); index >= 0 {
					matches = strings.EqualFold(item[:index], key)
				}
			}
			if !matches {
				envFiltered = append(envFiltered, item)
			}
		}
		env = append(envFiltered, prefix+value)
	}
	return env
}

func Run(repo string, args []string) error {
	if len(args) == 0 {
		return errors.New("run requires a command after --")
	}
	env, err := Environment(repo)
	if err != nil {
		return err
	}
	if len(env) == 0 {
		return errors.New("no build resources registered for this repository")
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = configuredEnvironment(env)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

type Probe struct {
	Kind       string `json:"kind"`
	Configured string `json:"configured"`
	Effective  string `json:"effective,omitempty"`
	Status     string `json:"status"`
	Detail     string `json:"detail,omitempty"`
}
type DoctorReport struct {
	OK        bool    `json:"ok"`
	Resources []Probe `json:"resources"`
}

func Doctor(repo string) (DoctorReport, error) {
	resources, err := List(repo)
	if err != nil {
		return DoctorReport{}, err
	}
	env, err := Environment(repo)
	if err != nil {
		return DoctorReport{}, err
	}
	report := DoctorReport{OK: len(resources) > 0, Resources: []Probe{}}
	for _, r := range resources {
		p := Probe{Kind: r.Kind, Configured: r.Path, Status: "configured"}
		if _, err := os.Stat(r.Path); err != nil {
			p.Status = "unavailable"
			p.Detail = "registered directory is missing or unreadable"
			report.OK = false
			report.Resources = append(report.Resources, p)
			continue
		}
		var args []string
		switch r.Kind {
		case "go-build":
			args = []string{"go", "env", "GOCACHE"}
		case "go-mod":
			args = []string{"go", "env", "GOMODCACHE"}
		case "uv-cache":
			args = []string{"uv", "cache", "dir"}
		case "pnpm-store":
			args = []string{"pnpm", "store", "path"}
		case "rust-target":
			p.Effective = env["CARGO_TARGET_DIR"]
			p.Detail = "environment configured; no Rust build was run"
		case "model":
			p.Effective = r.Path
			p.Detail = "registered directory exists; model contents were not validated"
		}
		if len(args) > 0 {
			ctx, cancel := probeContext()
			cmd := exec.CommandContext(ctx, args[0], args[1:]...)
			cmd.Dir = repo
			values := map[string]string{}
			for k, v := range env {
				values[k] = v
			}
			values["GOTOOLCHAIN"] = "local"
			values["COREPACK_ENABLE_NETWORK"] = "0"
			values["COREPACK_ENABLE_DOWNLOAD_PROMPT"] = "0"
			cmd.Env = configuredEnvironment(values)
			bytes, probeErr := cmd.Output()
			cancel()
			if probeErr != nil {
				p.Status = "unavailable"
				p.Detail = "tool probe failed or timed out: " + probeErr.Error()
				report.OK = false
			} else {
				p.Effective = strings.TrimSpace(string(bytes))
				effective, e := physical(p.Effective)
				expected, expectedErr := physical(r.Path)
				// pnpm store path adds its version-specific subdirectory below storeDir.
				match := effective == expected
				if r.Kind == "pnpm-store" {
					match = overlaps(expected, effective)
				}
				if e != nil || expectedErr != nil || !match {
					p.Status = "mismatch"
					p.Detail = "effective tool path differs from the registered root"
					report.OK = false
				} else {
					p.Status = "verified"
				}
			}
		}
		report.Resources = append(report.Resources, p)
	}
	sort.Slice(report.Resources, func(i, j int) bool { return report.Resources[i].Kind < report.Resources[j].Kind })
	return report, nil
}
