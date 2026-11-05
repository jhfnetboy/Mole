#!/usr/bin/env bats

load helpers/common

setup_file() {
    mole_test_setup_project_root
    DEV_CACHE_TEST_BINARY="$BATS_FILE_TMPDIR/dev-cache-go"
    export DEV_CACHE_TEST_BINARY
    go build -o "$DEV_CACHE_TEST_BINARY" "$PROJECT_ROOT/cmd/dev-cache"
}

setup() {
    export HOME="$BATS_TEST_TMPDIR/home"
    export DEV_CACHE_STATE_DIR="$HOME/state"
    export DEV_CACHE_DATA_DIR="$HOME/shared"
    export MOLE_TEST_NO_AUTH=1
    mkdir -p "$HOME/repo" "$HOME/.cache/uv" "$HOME/unrelated"
    git -C "$HOME/repo" init --quiet
    printf 'keep\n' > "$HOME/.cache/uv/keep"
    printf 'remove\n' > "$HOME/unrelated/remove"
}

register_cache() {
    "$DEV_CACHE_TEST_BINARY" register --repo "$HOME/repo" --kind uv-cache --path "$HOME/.cache/uv"
}

@test "registered resource protects its children and parent with an empty Mole whitelist" {
    register_cache
    run /bin/bash --noprofile --norc << 'EOF'
set -euo pipefail
source "$PROJECT_ROOT/lib/core/common.sh"
_MOLE_DEV_CACHE_BINARY="$DEV_CACHE_TEST_BINARY"
WHITELIST_PATTERNS=()
for path in "$HOME/.cache/uv" "$HOME/.cache/uv/keep" "$HOME/.cache"; do
    is_path_whitelisted "$path" || exit 1
    if safe_remove "$path" true; then exit 1; fi
done
[[ -f "$HOME/.cache/uv/keep" ]] || exit 1
safe_remove "$HOME/unrelated/remove" true
[[ ! -e "$HOME/unrelated/remove" ]] || exit 1
echo PROTECTED_AND_UNRELATED_REMOVED
EOF
    [ "$status" -eq 0 ] || {
        echo "$output"
        return 1
    }
    [[ "$output" == *PROTECTED_AND_UNRELATED_REMOVED* ]] || return 1
}

@test "registered resource is omitted from dry-run accounting" {
    register_cache
    run /bin/bash --noprofile --norc << 'EOF'
set -euo pipefail
source "$PROJECT_ROOT/lib/core/common.sh"
_MOLE_DEV_CACHE_BINARY="$DEV_CACHE_TEST_BINARY"
WHITELIST_PATTERNS=()
MOLE_DRY_RUN=1
record_dry_run_cleanup_target() { echo PREVIEWED >> "$HOME/preview.trace"; }
if mole_delete "$HOME/.cache/uv"; then exit 1; fi
mole_delete "$HOME/unrelated/remove"
[[ "$(wc -l < "$HOME/preview.trace" | tr -d ' ')" == 1 ]] || exit 1
[[ -f "$HOME/.cache/uv/keep" && -f "$HOME/unrelated/remove" ]] || exit 1
echo DRY_RUN_FILTERED
EOF
    [ "$status" -eq 0 ] || {
        echo "$output"
        return 1
    }
    [[ "$output" == *DRY_RUN_FILTERED* ]] || return 1
}

@test "registered uv cache prevents its owner cleanup while unregistered cache runs" {
    register_cache
    run /bin/bash --noprofile --norc << 'EOF'
set -euo pipefail
source "$PROJECT_ROOT/lib/core/common.sh"
source "$PROJECT_ROOT/lib/clean/dev.sh"
_MOLE_DEV_CACHE_BINARY="$DEV_CACHE_TEST_BINARY"
WHITELIST_PATTERNS=()
DRY_RUN=false
uv() {
    case "$*" in
        --version) return 0 ;;
        'cache dir') printf '%s\n' "$probe_root" ;;
        'cache prune') printf '%s\n' "$probe_root" >> "$HOME/owner.trace" ;;
        *) return 1 ;;
    esac
}
run_with_timeout() { shift; "$@"; }
note_activity() { :; }
probe_root="$HOME/.cache/uv"
clean_uv_cache
[[ ! -e "$HOME/owner.trace" ]] || exit 1
probe_root="$HOME/unrelated"
clean_uv_cache
[[ "$(cat "$HOME/owner.trace")" == "$HOME/unrelated" ]] || exit 1
echo OWNER_FILTERED
EOF
    [ "$status" -eq 0 ] || {
        echo "$output"
        return 1
    }
    [[ "$output" == *OWNER_FILTERED* ]] || return 1
}

@test "configured protection fails closed for malformed missing registry and missing binary" {
    register_cache
    run /bin/bash --noprofile --norc << 'EOF'
set -euo pipefail
source "$PROJECT_ROOT/lib/core/common.sh"
_MOLE_DEV_CACHE_BINARY="$DEV_CACHE_TEST_BINARY"
WHITELIST_PATTERNS=()
printf 'invalid-json\n' > "$DEV_CACHE_STATE_DIR/registry.json"
if safe_remove "$HOME/unrelated/remove" true; then exit 1; fi
/bin/mv "$DEV_CACHE_STATE_DIR/registry.json" "$DEV_CACHE_STATE_DIR/invalid.json"
if safe_remove "$HOME/unrelated/remove" true; then exit 1; fi
_MOLE_DEV_CACHE_BINARY="$HOME/missing-binary"
if safe_remove "$HOME/unrelated/remove" true; then exit 1; fi
[[ -f "$HOME/unrelated/remove" ]] || exit 1
echo UNKNOWN_KEPT
EOF
    [ "$status" -eq 0 ] || {
        echo "$output"
        return 1
    }
    [[ "$output" == *UNKNOWN_KEPT* ]] || return 1
}

@test "resource registered during sizing is kept by the final removal gate" {
    register_cache
    run /bin/bash --noprofile --norc << 'EOF'
set -euo pipefail
source "$PROJECT_ROOT/lib/core/common.sh"
_MOLE_DEV_CACHE_BINARY="$DEV_CACHE_TEST_BINARY"
WHITELIST_PATTERNS=()
get_path_size_kb() {
    "$DEV_CACHE_TEST_BINARY" register --repo "$HOME/repo" --kind model --name late \
        --path "$HOME/unrelated" > /dev/null
    printf '1\n'
}
if safe_remove "$HOME/unrelated" true; then exit 1; fi
[[ -f "$HOME/unrelated/remove" ]] || exit 1
"$DEV_CACHE_TEST_BINARY" list --repo "$HOME/repo"
echo LATE_REGISTRATION_KEPT
EOF
    [ "$status" -eq 0 ] || {
        echo "$output"
        return 1
    }
    [[ "$output" == *LATE_REGISTRATION_KEPT* && "$output" == *late* ]] || return 1
}

@test "Homebrew broad owner cleanup is skipped while the protocol is configured" {
    register_cache
    run /bin/bash --noprofile --norc << 'EOF'
set -euo pipefail
source "$PROJECT_ROOT/lib/core/common.sh"
source "$PROJECT_ROOT/lib/clean/brew.sh"
_MOLE_DEV_CACHE_BINARY="$DEV_CACHE_TEST_BINARY"
brew() { printf 'CALLED\n' >> "$HOME/brew.trace"; }
note_activity() { :; }
DRY_RUN=true
clean_homebrew
DRY_RUN=false
clean_homebrew
[[ ! -e "$HOME/brew.trace" ]] || exit 1
echo BROAD_OWNER_KEPT
EOF
    [ "$status" -eq 0 ] || {
        echo "$output"
        return 1
    }
    [[ "$output" == *BROAD_OWNER_KEPT* ]] || return 1
}
