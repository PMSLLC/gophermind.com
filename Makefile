BINARY := gophermind

.PHONY: build test vet check snapshot release clean

build: ## Build a local (unstamped) binary
	go build -o $(BINARY) ./cmd/gophermind

test: ## Run the full test suite
	go test ./...

vet:
	go vet ./...

check: ## Validate the GoReleaser config
	goreleaser check

snapshot: ## Dry-run release: build + archive + cask, no sign/notarize/publish
	goreleaser release --snapshot --clean --skip=sign

# Full signed + notarized release to GitHub + the Homebrew tap. Requires a pushed
# git tag plus signing env; GITHUB_TOKEN is auto-sourced from `gh` if unset.
# See docs/RELEASING.md.
release: ## Cut a full signed+notarized release
	@: $${MACOS_SIGN_IDENTITY:?set MACOS_SIGN_IDENTITY, e.g. \"Developer ID Application: Your Name (TEAMID)\" — see docs/RELEASING.md}
	@: $${MACOS_NOTARY_PROFILE:?set MACOS_NOTARY_PROFILE to your notarytool keychain profile — see docs/RELEASING.md}
	GITHUB_TOKEN="$${GITHUB_TOKEN:-$$(gh auth token 2>/dev/null)}" goreleaser release --clean

clean:
	rm -rf dist $(BINARY)
