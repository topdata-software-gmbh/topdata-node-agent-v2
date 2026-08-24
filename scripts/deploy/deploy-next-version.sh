#!/usr/bin/env bash
#
# deploy-next-version.sh - Interactive semantic-version release helper.
#
# Calculates the next Patch / Minor / Major version from the highest existing
# git tag (vX.Y.Z), presents an arrow-key menu, then:
#   1. rotates CHANGELOG.md ([Unreleased] -> [vX.Y.Z] - DATE, fresh [Unreleased])
#   2. commits the changelog rotation
#   3. creates an annotated tag at that commit and pushes it (plus main)
#   4. runs deploy/deploy-to-prod.sh, which builds the exact version (via
#      `git describe`) and rolls it out to all hosts with ansible.
#
# Usage:
#   ./scripts/deploy/deploy-next-version.sh [ansible-playbook args...]
#
# Any argument is passed through to deploy/deploy-to-prod.sh unchanged
# (e.g. `--limit arm1` to test a single server first).
#
# 2026 created
# Author: AI (opencode)
set -euo pipefail
# `-e`: Exit immediately on error
# `-u`: Treat unset variables as an error
# `-o pipefail`: Return exit status of the last command in a pipe that failed

# ---- usage function
usage() {
    cat <<'EOF'
Usage: scripts/deploy/deploy-next-version.sh [ansible-playbook args...]

Interactive semantic-version release helper. Calculates the next Patch / Minor
/ Major version from the highest existing git tag, rotates the CHANGELOG,
creates an annotated tag, pushes it, and triggers deploy/deploy-to-prod.sh to
build + deploy that exact version.

Selecting the first menu entry ("Deploy existing vX.Y.Z") skips the tag +
CHANGELOG step and re-deploys the latest existing tag as-is (handy for a
single-server test via --limit arm1).

Any argument (e.g. --limit arm1) is forwarded to deploy/deploy-to-prod.sh.
EOF
}

# ── helpers ─────────────────────────────────────────────────────────────────
die()  { echo -e "\033[31mError:\033[0m $*" >&2; exit 1; }
warn() { echo -e "\033[33mWarning:\033[0m $*"; }

# ---- vars
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
PROJECT_ROOT=$(realpath "$SCRIPT_DIR/../..")
DEPLOY_SCRIPT="$PROJECT_ROOT/deploy/deploy-to-prod.sh"
CHANGELOG="$PROJECT_ROOT/CHANGELOG.md"
ANSIBLE_ARGS=()

for arg in "$@"; do
    case "$arg" in
        -h|--help) usage; exit 0 ;;
        *) ANSIBLE_ARGS+=("$arg") ;;
    esac
done

# ── safety: restore cursor on any exit ──────────────────────────────────────
trap 'printf "\033[?25h"' EXIT

# ── pre-flight checks ────────────────────────────────────────────────────────
command -v git > /dev/null 2>&1 || die "git is not installed."
git rev-parse --is-inside-work-tree > /dev/null 2>&1 \
    || die "Must be run from within a git repository."
[ -f "$DEPLOY_SCRIPT" ] || die "deploy script not found at $DEPLOY_SCRIPT"
[ -f "$CHANGELOG" ] || die "CHANGELOG.md not found at $CHANGELOG"

cd "$PROJECT_ROOT" || die "Failed to change directory to $PROJECT_ROOT"

# ── sync remote refs ─────────────────────────────────────────────────────────
echo "Fetching latest tags and refs from origin…"
git fetch origin --tags --prune --quiet
git fetch origin main --quiet

# ── dirty working tree guard ─────────────────────────────────────────────────
if [ -n "$(git status --porcelain)" ]; then
    warn "Your working directory has uncommitted changes."
    read -rp "Proceed anyway? (Y/n) " _reply
    echo
    if [[ "$_reply" =~ ^[Nn]$ ]]; then
        echo "Aborted."; exit 0
    fi
fi

# ── ensure HEAD is on origin/main ────────────────────────────────────────────
LOCAL_SHA=$(git rev-parse HEAD)
ORIGIN_MAIN_SHA=$(git rev-parse origin/main 2>/dev/null) \
    || die "Could not resolve origin/main. Is the remote reachable?"

if [ "$LOCAL_SHA" != "$ORIGIN_MAIN_SHA" ]; then
    die "HEAD ($LOCAL_SHA) is not at origin/main ($ORIGIN_MAIN_SHA).\nPlease push / pull your branch before releasing."
fi

# ── determine current version ────────────────────────────────────────────────
get_current_version() {
    local latest
    latest=$(git tag -l "v*" \
        | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' \
        | sort -V \
        | tail -n 1)
    if [ -z "$latest" ]; then
        echo "1.0.0"
    else
        echo "${latest#v}"
    fi
}

CURRENT=$(get_current_version)
IFS='.' read -r MAJOR MINOR PATCH <<< "$CURRENT"

NEXT_PATCH="$MAJOR.$MINOR.$((PATCH + 1))"
NEXT_MINOR="$MAJOR.$((MINOR + 1)).0"
NEXT_MAJOR="$((MAJOR + 1)).0.0"

# ── interactive arrow-key menu ───────────────────────────────────────────────
OPTIONS=(
    "Deploy existing v$CURRENT — no new tag, no CHANGELOG rotation"
    "Patch        $NEXT_PATCH         — backward-compatible bug fixes"
    "Minor        $NEXT_MINOR         — backward-compatible new features"
    "Major        $NEXT_MAJOR         — breaking / incompatible API changes"
)
NUM_OPTS=${#OPTIONS[@]}
SELECTED=0

render_menu() {
    printf "\033[?25l"   # hide cursor
    echo -e "? Current version is \033[36mv$CURRENT\033[0m — choose the release increment:"
    for i in "${!OPTIONS[@]}"; do
        if [ "$i" -eq "$SELECTED" ]; then
            echo -e "\033[36m❯ ${OPTIONS[$i]}\033[0m"
        else
            echo    "  ${OPTIONS[$i]}"
        fi
    done
}

clear_menu() {
    # move up (NUM_OPTS + 1 header) lines, then clear to end of screen
    printf "\033[%dA\033[J" $(( NUM_OPTS + 1 ))
}

render_menu

while true; do
    IFS= read -rsn1 KEY
    case "$KEY" in
        $'\x1B')
            IFS= read -rsn2 -t 0.1 SEQ || true
            case "$SEQ" in
                '[A')  # up
                    (( SELECTED-- )) || true
                    [ "$SELECTED" -lt 0 ] && SELECTED=$(( NUM_OPTS - 1 ))
                    clear_menu; render_menu ;;
                '[B')  # down
                    (( SELECTED++ )) || true
                    [ "$SELECTED" -ge "$NUM_OPTS" ] && SELECTED=0
                    clear_menu; render_menu ;;
            esac ;;
        '')  # Enter
            printf "\033[?25h"   # restore cursor
            break ;;
    esac
done

echo

# ── act on selection ─────────────────────────────────────────────────────────
DEPLOY_EXISTING=0
case "$SELECTED" in
    0)
        # Re-deploy the latest existing tag unchanged (e.g. test on a single
        # host via `--limit arm1`); no CHANGELOG rotation, no new tag.
        DEPLOY_EXISTING=1
        NEXT_VERSION="$CURRENT"
        ;;
    1) NEXT_VERSION="$NEXT_PATCH" ;;
    2) NEXT_VERSION="$NEXT_MINOR" ;;
    3) NEXT_VERSION="$NEXT_MAJOR" ;;
esac

TAG_NAME="v$NEXT_VERSION"
RELEASE_DATE="$(date +%Y-%m-%d)"

if [ "$DEPLOY_EXISTING" -eq 1 ]; then
    # Validate the tag actually exists before redeploying it.
    git tag -l "$TAG_NAME" | grep -q "^${TAG_NAME}$" \
        || die "Tag $TAG_NAME does not exist locally — cannot redeploy it."

    # ── final confirmation ───────────────────────────────────────────────────
    echo -e "Ready to deploy existing \033[32m$TAG_NAME\033[0m unchanged"
    read -rp "Build at $TAG_NAME and deploy? (Y/n) " _confirm
    echo
    if [[ "$_confirm" =~ ^[Nn]$ ]]; then
        echo "Aborted."; exit 0
    fi

    # ── build + deploy the exact existing tag ────────────────────────────────
    # Check out the tag (detached HEAD) so `git describe` bakes the exact
    # version, then restore the previous ref afterwards.
    PREV_REF="$(git rev-parse --abbrev-ref HEAD)"
    [ "$PREV_REF" = "HEAD" ] && PREV_REF="$(git rev-parse HEAD)"
    echo "Checking out $TAG_NAME for build…"
    git checkout --quiet "$TAG_NAME"
    trap 'git checkout --quiet '"$PREV_REF"' 2>/dev/null || true; printf "\033[?25h"' EXIT

    echo "Running deploy/deploy-to-prod.sh (version via git describe)…"
    "$DEPLOY_SCRIPT" "${ANSIBLE_ARGS[@]}"

    echo -e "\033[32m✓ Deployed existing $TAG_NAME.\033[0m"
else
    # Abort if tag already exists
    if git tag -l "$TAG_NAME" | grep -q "^${TAG_NAME}$"; then
        die "Tag $TAG_NAME already exists locally or on origin."
    fi

    # ── final confirmation ───────────────────────────────────────────────────
    echo -e "Ready to release \033[32m$TAG_NAME\033[0m"
    read -rp "Rotate CHANGELOG, tag, push, and deploy? (Y/n) " _confirm
    echo
    if [[ "$_confirm" =~ ^[Nn]$ ]]; then
        echo "Aborted."; exit 0
    fi

    # ── rotate CHANGELOG.md ──────────────────────────────────────────────────
    # Replace the first `## [Unreleased]` heading with `## [vX.Y.Z] - DATE`, and
    # insert a fresh `## [Unreleased]` before the next (previous release) heading.
    echo "Rotating CHANGELOG.md → $TAG_NAME…"
    awk -v ver="## [$TAG_NAME] - $RELEASE_DATE" '
        BEGIN { seen=0; inserted=0 }
        /^## / && !seen {
            sub(/^## \[Unreleased\]$/, ver)
            seen=1
            print
            next
        }
        /^## / && seen && !inserted {
            print "## [Unreleased]"
            print ""
            inserted=1
        }
        { print }
        END {
            if (seen && !inserted) {
                print "## [Unreleased]"
                print ""
            }
        }
    ' "$CHANGELOG" > "$CHANGELOG.tmp" && mv "$CHANGELOG.tmp" "$CHANGELOG"

    git add "$CHANGELOG"
    git commit -m "chore(release): $TAG_NAME"
    echo -e "\033[32m✓ CHANGELOG rotated and committed.\033[0m"

    # ── create + push tag (at the changelog commit) ──────────────────────────
    echo "Creating annotated tag $TAG_NAME…"
    git tag -a "$TAG_NAME" -m "Release $TAG_NAME"

    echo "Pushing main and tag to origin…"
    git push origin main
    git push origin "$TAG_NAME"

    echo -e "\033[32m✓ Tag $TAG_NAME pushed.\033[0m"

    # ── build + deploy that exact version ────────────────────────────────────
    echo "Running deploy/deploy-to-prod.sh (version via git describe)…"
    "$DEPLOY_SCRIPT" "${ANSIBLE_ARGS[@]}"

    echo -e "\033[32m✓ Released and deployed $TAG_NAME.\033[0m"
fi
