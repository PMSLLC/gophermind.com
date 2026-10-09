#!/usr/bin/env bash
# Clear the state of one /project attempt (spec section 9, runbook section 4).
#
# Usage: scripts/clear-project-state.sh <repo> <id> <work-branch> <archive-dir>
#   <repo>         absolute path of the target repo (not /, not $HOME, not an
#                  ancestor of $HOME, no .. component; symlinks are resolved)
#   <id>           the brief id, gm-YYYY-MM-DD-NNN
#   <work-branch>  the git_branch_delete line of --print-state-paths
#                  (gm/<id> unless the brief sets work_branch)
#   <archive-dir>  absolute, existing directory outside the repo; it receives
#                  the tar.gz of the WHOLE <repo>/.gophermind folder
# Env:   BASE_BRANCH  the branch HEAD is put on (default main)
#        BASELINE     the revision the repo is reset to (default goal-baseline)
#        GOPHERMIND_CONFIG_DIR  holds runs/<id>.json (default $HOME/.gophermind)
#
# Every argument is checked before the first git command runs, so an empty or
# mistyped path never resets or cleans the current directory. Lines tagged
# "guard:" are exercised one by one by projectrun/clear_script_test.go, which
# removes each in a copy of this file and expects its case to fail.
set -euo pipefail

die() { echo "clear-project-state: $*" >&2; exit 1; }

# ref_ok NAME: a conservative git ref name (letters, digits, / _ . -), at most
# 200 characters, no leading - or /, no .., //, /., @{, trailing . or / or .lock
ref_ok() {
  local n="$1"
  [ "${#n}" -ge 1 ] && [ "${#n}" -le 200 ] || return 1
  [[ "$n" =~ ^[A-Za-z0-9_][A-Za-z0-9_./-]*$ ]] || return 1
  case "$n" in *..* | *//* | */.* | *.lock | */ | *.) return 1 ;; esac
  return 0
}

[ "$#" -eq 4 ] || die "usage: clear-project-state.sh <repo> <id> <work-branch> <archive-dir>" # guard:args
R_ARG="$1"; ID="$2"; BR="$3"; ARCHIVE="$4"
BASE_BRANCH="${BASE_BRANCH:-main}"
BASELINE="${BASELINE:-goal-baseline}"
CFG="${GOPHERMIND_CONFIG_DIR:-${HOME:-}/.gophermind}"

[ -n "$R_ARG" ] || die "repo is empty" # guard:empty-repo
case "$R_ARG" in /*) ;; *) die "repo must be an absolute path" ;; esac # guard:abs
case "/$R_ARG/" in */../*) die "repo must not contain a .. component" ;; esac # guard:dotdot
R="$(cd -- "$R_ARG" 2>/dev/null && pwd -P)" || die "repo does not resolve to a directory" # guard:canon
[ -n "$R" ] || die "repo does not resolve to a directory"
HOME_DIR="$(cd -- "${HOME:-/nonexistent}" 2>/dev/null && pwd -P)" || die "\$HOME does not resolve to a directory"
case "$R" in *[!/]*) ;; *) die "repo must not be /" ;; esac # guard:root
[ "$R" != "$HOME_DIR" ] || die "repo must not be \$HOME" # guard:home
case "$HOME_DIR/" in "$R"/?*) die "repo must not contain \$HOME" ;; esac # guard:ancestor
[ -d "$R/.git" ] || die "repo has no .git directory" # guard:nogit
[[ "$ID" =~ ^gm-[0-9]{4}-[0-9]{2}-[0-9]{2}-[0-9]{3}$ ]] || die "id is not gm-YYYY-MM-DD-NNN" # guard:id
[ -n "$BR" ] || die "work branch is empty" # guard:branch-empty
ref_ok "$BR" || die "work branch is not a valid branch name" # guard:branch-form
ref_ok "$BASE_BRANCH" || die "BASE_BRANCH is not a valid branch name" # guard:base-form
ref_ok "$BASELINE" || die "BASELINE is not a valid revision name" # guard:baseline-form
[ "$BR" != "$BASE_BRANCH" ] || die "work branch equals the base branch" # guard:branch-base
case "$ARCHIVE" in /*) ;; *) die "archive dir must be an absolute path" ;; esac # guard:archive-abs
[ -d "$ARCHIVE" ] || die "archive dir does not exist" # guard:archive-exists
ARCHIVE="$(cd -- "$ARCHIVE" && pwd -P)"
case "$ARCHIVE/" in "$R"/*) die "archive dir must be outside the repo" ;; esac # guard:archive-inside
case "$CFG" in /*) ;; *) die "config dir must be an absolute path" ;; esac
[ ! -L "$R/.gophermind" ] || die "repo .gophermind must not be a symlink" # guard:gm-symlink
git -C "$R" rev-parse --verify --quiet "$BASELINE^{commit}" >/dev/null || die "BASELINE does not name a commit" # guard:baseline-exists
git -C "$R" rev-parse --verify --quiet "refs/heads/$BASE_BRANCH" >/dev/null || die "BASE_BRANCH does not name a branch" # guard:base-exists

run_dir="$R/.gophermind/$ID"
scratch_dir="$R/.gophermind/$ID-scratch"
record="$CFG/runs/$ID.json"

# 0. archive first, before any destructive step. The whole .gophermind folder
# (every run id) goes into a tar.gz, because git clean -fdx removes untracked
# files. It is skipped when the folder does not exist.
if [ -d "$R/.gophermind" ]; then
  tar -C "$R" -czf "$ARCHIVE/attempt-$ID.tar.gz" .gophermind
fi
# the work branch commits survive in a bundle, and its tip is recorded
if git -C "$R" rev-parse --verify --quiet "refs/heads/$BR" >/dev/null; then
  git -C "$R" rev-parse "refs/heads/$BR" >"$ARCHIVE/work-branch-tip.txt"
  git -C "$R" bundle create "$ARCHIVE/work-branch.bundle" "refs/heads/$BR"
fi

git -C "$R" symbolic-ref HEAD "refs/heads/$BASE_BRANCH"
if git -C "$R" branch --list "$BR" | grep -q .; then
  git -C "$R" branch -D "$BR"
fi
git -C "$R" reset --hard "$BASELINE"
# git clean -fdx removes untracked AND ignored files (.env, build output); only
# .remember is kept. The names it will remove are recorded first (names only).
git -C "$R" clean -ndx -e .remember >"$ARCHIVE/removed-files.txt"
git -C "$R" clean -fdx -e .remember

# what clean left behind (git may not be resetting this repo), each exact path
[ ! -L "$R/.gophermind" ] || die "repo .gophermind became a symlink" # guard:gm-symlink-late
for target in "$run_dir" "$scratch_dir"; do
  [ ! -L "$target" ] || die "$target is a symlink" # guard:rm-target
  if [ -d "$target" ]; then
    [ "$(cd -- "$(dirname -- "$target")" && pwd -P)" = "$R/.gophermind" ] || die "$target is not inside the repo .gophermind" # guard:rm-parent
    rm -rf -- "${target:?}"
  fi
done
if [ -f "$record" ]; then
  rm -f -- "${record:?}"
fi
echo "cleared $ID in $R"
