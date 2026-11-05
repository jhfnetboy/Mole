// dev-cache registers reusable resources and provides their build environment.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/tw93/mole/internal/devcache"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, out, errOut io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(out, "Usage: dev-cache register|list|env|run|doctor|guard [options]\nregister --repo PATH --kind pnpm-store|uv-cache|go-build|go-mod|rust-target|model [--path DIR] [--name NAME]\nlist [--repo PATH]\nenv|doctor --repo PATH\nrun --repo PATH -- COMMAND ARGS\nguard --path PATH (0 unrelated, 3 protected, 2 invalid registry)")
		return 0
	}
	command := args[0]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(errOut)
	repo := ""
	kind := ""
	path := ""
	name := ""
	switch command {
	case "register":
		flags.StringVar(&repo, "repo", "", "repository directory")
		flags.StringVar(&kind, "kind", "", "resource kind")
		flags.StringVar(&path, "path", "", "existing reusable directory")
		flags.StringVar(&name, "name", "", "resource name")
	case "list", "env", "run", "doctor":
		flags.StringVar(&repo, "repo", "", "repository directory")
	case "guard":
		flags.StringVar(&path, "path", "", "candidate cleanup path")
	default:
		fmt.Fprintln(errOut, "unknown command:", command)
		return 2
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if command != "run" && len(flags.Args()) != 0 {
		fmt.Fprintln(errOut, "unexpected positional arguments")
		return 2
	}
	if command != "list" && command != "guard" && repo == "" {
		fmt.Fprintln(errOut, "--repo is required")
		return 2
	}
	if command == "guard" && path == "" {
		fmt.Fprintln(errOut, "--path is required")
		return 2
	}
	var result any
	var err error
	status := 0
	switch command {
	case "register":
		result, err = devcache.Register(repo, kind, path, name)
	case "list":
		result, err = devcache.List(repo)
	case "env":
		result, err = devcache.Environment(repo)
	case "doctor":
		var report devcache.DoctorReport
		report, err = devcache.Doctor(repo)
		result = report
		if !report.OK {
			status = 1
		}
	case "run":
		err = devcache.Run(repo, flags.Args())
		if err == nil {
			return 0
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
	case "guard":
		err = devcache.CheckPath(path)
		if err == nil {
			result = map[string]any{"protected": false}
		} else if errors.Is(err, devcache.ErrProtected) {
			result = map[string]any{"protected": true, "reason": err.Error()}
			err = nil
			status = 3
		}
	}
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 2
	}
	if err = json.NewEncoder(out).Encode(result); err != nil {
		fmt.Fprintln(errOut, err)
		return 2
	}
	return status
}
