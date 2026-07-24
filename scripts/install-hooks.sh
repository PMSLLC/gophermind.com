#!/usr/bin/env bash
# Install a git pre-push hook that runs the comprehensive test gate before any
# push. A push is the upstream of every deployment, so gating pushes closes the
# gap where someone pushes red code that a later deploy then ships.
#
# Bypass for a genuine emergency: git push --no-verify (deliberate and visible).
set -euo pipefail
cd "$(dirname "$0")/.."

hook=".git/hooks/pre-push"
cat > "$hook" <<'HOOK'
#!/usr/bin/env bash
# gophermind pre-push gate — installed by scripts/install-hooks.sh.
# Runs the full pre-deploy test gate; a failure blocks the push.
# Emergency bypass: git push --no-verify
echo "running pre-push test gate (git push --no-verify to bypass)…"
exec ./scripts/predeploy.sh
HOOK
chmod +x "$hook"
echo "✓ installed pre-push hook at $hook"
echo "  it runs ./scripts/predeploy.sh before every push; bypass with --no-verify"
