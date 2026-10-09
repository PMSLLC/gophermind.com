#!/usr/bin/env bash
# Clear the state of one /project attempt (spec section 9, runbook section 4).
#
# Usage: scripts/clear-project-state.sh <repo> <id> <work-branch> <archive-dir>
#   <repo>         absolute path of the target repo (not / and not $HOME)
#   <id>           the brief id, gm-YYYY-MM-DD-NNN
#   <work-branch>  the git_branch_delete line of --print-state-paths
#                  (gm/<id> unless the brief sets work_branch)
#   <archive-dir>  absolute, existing directory that receives the tar.gz of the run folder
# Env:   BASE_BRANCH  the branch HEAD is put on (default main)
#        BASELINE     the revision the repo is reset to (default goal-baseline)
#        GOPHERMIND_CONFIG_DIR  holds runs/<id>.json (default $HOME/.gophermind)
#
# Every argument is checked before the first git command runs, so an empty or
# mistyped path never resets or cleans the current directory.
set -euo pipefail

die() { echo "clear-project-state: $*" >&2; exit 1; }

[ "$#" -eq 4 ] || die "usage: clear-project-state.sh <repo> <id> <work-branch> <archive-dir>"
R="$1"; ID="$2"; BR="$3"; ARCHIVE="$4"
BASE_BRANCH="${BASE_BRANCH:-main}"
BASELINE="${BASELINE:-goal-baseline}"
CFG="${GOPHERMIND_CONFIG_DIR:-$HOME/.gophermind}"

[ -n "$R" ] || die "repo is empty"
case "$R" in /*) ;; *) die "repo must be an absolute path" ;; esac
[ "$R" != "/" ] || die "repo must not be /"
[ "$R" != "$HOME" ] || die "repo must not be \$HOME"
[ -d "$R/.git" ] || die "repo has no .git directory"
[[ "$ID" =~ ^gm-[0-9]{4}-[0-9]{2}-[0-9]{2}-[0-9]{3}$ ]] || die "id is not gm-YYYY-MM-DD-NNN"
[ -n "$BR" ] || die "work branch is empty"
case "$BR" in -*) die "work branch must not start with a dash" ;; esac
git check-ref-format --branch "$BR" >/dev/null 2>&1 || die "work branch is not a valid branch name"
[ "$BR" != "$BASE_BRANCH" ] || die "work branch equals the base branch"
case "$ARCHIVE" in /*) ;; *) die "archive dir must be an absolute path" ;; esac
[ -d "$ARCHIVE" ] || die "archive dir does not exist"
case "$CFG" in /*) ;; *) die "config dir must be an absolute path" ;; esac

run_dir="$R/.gophermind/$ID"
scratch_dir="$R/.gophermind/$ID-scratch"
record="$CFG/runs/$ID.json"

# 0. archive the whole run folder first: clearing destroys the ledger, the events and the attempts
if [ -d "$run_dir" ]; then
  tar -C "$R/.gophermind" -czf "$ARCHIVE/attempt-$ID.tar.gz" "$ID"
fi

git -C "$R" symbolic-ref HEAD "refs/heads/$BASE_BRANCH"
if git -C "$R" branch --list "$BR" | grep -q .; then
  git -C "$R" branch -D "$BR"
fi
git -C "$R" reset --hard "$BASELINE"
git -C "$R" clean -fdx -e .remember

# what clean left behind (git may not be resetting this repo), each exact path
for target in "$run_dir" "$scratch_dir"; do
  if [ -d "$target" ]; then
    rm -rf -- "${target:?}"
  fi
done
if [ -f "$record" ]; then
  rm -f -- "${record:?}"
fi
echo "cleared $ID in $R"
