---
name: dev-cache
description: Register a repository's reusable developer caches or model directories, get shared build configuration, and verify effective paths with this Mole fork's dev-cache CLI. Use when onboarding a repository to shared pnpm, uv, Go, or Rust storage, or protecting model weights before cleanup. Does not authorize cleanup or automatically migrate existing data.
---

# Developer cache agreement

Use the executable at `scripts/dev-cache` relative to this skill's physical
directory. It resolves this fork's CLI independently of the current working
directory. When installed globally by this fork, the usual entry point is
`~/.codex/skills/dev-cache/scripts/dev-cache` (or `$CODEX_HOME/skills/dev-cache/scripts/dev-cache`).

## Onboard a repository

1. Identify the absolute Git checkout path and its package managers. Inspect
   their existing cache configuration before choosing new storage. Do not move
   caches or alter shell profiles as part of registration.
2. Run `list --repo <checkout>` first. Register only the kinds the project
   actually uses. With no `--path`, the CLI allocates managed storage; use
   `--path <existing-directory>` to preserve an existing model or cache in place.
3. Use `env --repo <checkout>` to inspect the resulting JSON configuration.
   Run a requested build/install through `run --repo <checkout> -- <command>`;
   never feed the JSON to `eval`. Running a build or install still needs
   authorization from the task; registration alone does not authorize it.
   `--repo` selects the registration; `run` preserves the caller's working
   directory. Run it from the intended checkout, or use the command's own
   explicit directory option where supported.
4. Run `doctor --repo <checkout>` to check actual tool settings. A missing tool,
   unsupported probe, or conflicting configuration is unresolved, not proof of
   reuse. Do not install or download a toolchain to make the check pass unless
   the user requested installation.
5. For durable onboarding, add this agreement to the repository's `AGENTS.md`
   and its normal build entry point when authorized. A discoverable global
   skill does not force other agents or arbitrary shell commands to use it.

Supported kinds:

| Kind | Purpose |
|---|---|
| `pnpm-store` | Shared package store; project dependency trees remain separate |
| `uv-cache` | Shared package/wheel cache; virtual environments remain separate |
| `go-build` | Go build cache (`GOCACHE`) |
| `go-mod` | Go module cache (`GOMODCACHE`) |
| `rust-target` | Build directory isolated per checkout/worktree |
| `model` | Existing model directory, registered for protection only |

Examples below use a shell variable pointing to this skill's executable:

```bash
dev_cache="$HOME/.codex/skills/dev-cache/scripts/dev-cache"
"$dev_cache" list --repo /absolute/path/to/repository
"$dev_cache" register --repo /absolute/path/to/repository --kind uv-cache
"$dev_cache" register --repo /absolute/path/to/repository --kind go-build
"$dev_cache" register --repo /absolute/path/to/repository --kind go-mod
"$dev_cache" register --repo /absolute/path/to/repository --kind model \
  --path /absolute/path/to/existing/models --name speech-models
"$dev_cache" env --repo /absolute/path/to/repository
"$dev_cache" doctor --repo /absolute/path/to/repository
# Only when the task authorizes this build:
"$dev_cache" run --repo /absolute/path/to/repository -- go build ./...
```

## Protection and limits

- The registry defaults to `~/.local/state/dev-cache/registry.json`; allocated
  storage defaults to `~/.local/share/dev-cache`. `DEV_CACHE_STATE_DIR` and
  `DEV_CACHE_DATA_DIR` override these for isolated tests or explicit configuration.
- Registrations are persistent protections. An inactive checkout or an old
  timestamp does not authorize removal. There is no unregister, eviction, or
  cleanup command in this first stage.
- Unregistered is **unknown**, not disposable. Downloaded models, installed
  tools, authored files and live environments are not ordinary temporary files.
- Do not use generic `mo clean`, `mo purge`, an owner-tool cache cleaner, or
  direct deletion as an onboarding step. This skill does not authorize them.
- Shared downloads and compatible build-cache entries can be reused; identical
  directory names do not make arbitrary Rust/TS build outputs interchangeable.
  Cross-project compiler caching and model revision management are later work.
- A malformed/unreadable registry or a missing guard binary must retain affected
  paths. Never disable the guard to continue cleanup.
