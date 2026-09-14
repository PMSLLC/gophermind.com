#!/usr/bin/env bash
# Deploy desktop app: commit changes, push, rebuild, install to /Applications
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

echo "==> Desktop App Deploy"
echo

# 1. Check for uncommitted changes
echo "Checking git status..."
if ! git diff-index --quiet HEAD --; then
  echo "Staging changes..."
  git add -A
fi

if git diff-index --cached --quiet HEAD --; then
  echo "No changes to commit"
else
  echo "Committing changes..."
  commit_msg="deploy: desktop app update with latest fixes"
  git commit -m "$commit_msg" || true
fi

# 2. Push to remote
echo "Pushing to remote..."
git push origin main || echo "Push failed (you may need to set up remote)"

# 3. Build desktop app
echo
echo "Building desktop app..."
cd desktop
wails build -platform darwin/universal -o "GopherMind Desktop" 2>&1 | tail -10
cd "$repo_root"

# 4. Install to /Applications
echo
echo "Installing to /Applications..."
rm -rf "/Applications/GopherMind Desktop.app"
cp -r "desktop/build/bin/GopherMind Desktop.app" /Applications/

echo
echo "✅ Desktop app deployed to /Applications"
echo "Restart the app to load the changes."
