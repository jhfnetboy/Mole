#!/bin/bash
# Registered reusable resources are protection policy, never deletion authority.

if [[ -n "${MOLE_DEV_CACHE_LOADED:-}" ]]; then
    return 0
fi
readonly MOLE_DEV_CACHE_LOADED=1
_MOLE_DEV_CACHE_BINARY="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/bin/dev-cache-go"

# A missing state root opts out. An unreadable ancestor is unknown, not absent.
mole_dev_cache_configured() {
    local state_dir="${DEV_CACHE_STATE_DIR:-$HOME/.local/state/dev-cache}"
    [[ "$state_dir" == /* ]] || return 0
    [[ -e "$state_dir" || -L "$state_dir" ]] && return 0
    local ancestor="${state_dir%/*}"
    while [[ -n "$ancestor" && "$ancestor" != / && ! -e "$ancestor" && ! -L "$ancestor" ]]; do
        ancestor="${ancestor%/*}"
    done
    [[ -n "$ancestor" ]] || ancestor=/
    [[ -d "$ancestor" && -x "$ancestor" ]] || return 0
    return 1
}

# True means protected OR unknown. Keep every caller fail-closed, including
# previews, owner commands and final sinks; never cache registry decisions.
mole_dev_cache_protects() {
    local target="$1"
    mole_dev_cache_configured || return 1
    local guard_rc=2
    local detail="dev-cache helper unavailable; run make build in the Mole fork"
    if [[ -x "$_MOLE_DEV_CACHE_BINARY" ]] && declare -f run_with_timeout > /dev/null 2>&1; then
        detail=$(run_with_timeout "${MOLE_TIMEOUT_QUICK_DETECT_SEC:-3}" \
            "$_MOLE_DEV_CACHE_BINARY" guard --path "$target" 2>&1) && guard_rc=0 || guard_rc=$?
    fi
    [[ $guard_rc -ne 0 ]] || return 1
    if [[ $guard_rc -ne 3 && -z "${_MOLE_DEV_CACHE_WARNING_SHOWN:-}" ]]; then
        _MOLE_DEV_CACHE_WARNING_SHOWN=1
        printf 'Mole kept files because dev-cache protection could not be checked: %s\n' "$detail" >&2
    fi
    return 0
}
