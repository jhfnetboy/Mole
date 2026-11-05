#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd -P "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -P "$SCRIPT_DIR/.." && pwd)"
SKILL_SOURCE="$REPO_ROOT/skills/dev-cache"

if [[ $# -gt 1 ]]; then
    printf '%s\n' 'Usage: scripts/install-dev-cache-skill.sh [destination-skill-directory]' >&2
    exit 2
fi

SKILL_DESTINATION="${1:-${CODEX_HOME:-$HOME/.codex}/skills/dev-cache}"
if [[ "$SKILL_DESTINATION" != /* ]]; then
    printf '%s\n' 'The destination must be an absolute skill-directory path.' >&2
    exit 2
fi

if [[ -L "$SKILL_DESTINATION" ]] && [[ "$(readlink "$SKILL_DESTINATION")" == "$SKILL_SOURCE" ]]; then
    printf 'Already installed: %s -> %s\n' "$SKILL_DESTINATION" "$SKILL_SOURCE"
    exit 0
fi

if [[ -e "$SKILL_DESTINATION" || -L "$SKILL_DESTINATION" ]]; then
    printf 'Refusing to replace an existing skill: %s\n' "$SKILL_DESTINATION" >&2
    exit 2
fi

mkdir -p "$(dirname "$SKILL_DESTINATION")"
ln -s "$SKILL_SOURCE" "$SKILL_DESTINATION"
printf 'Installed: %s -> %s\n' "$SKILL_DESTINATION" "$SKILL_SOURCE"
