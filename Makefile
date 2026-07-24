BINARY := gophermind

.PHONY: build test vet check snapshot release clean ios-test ios-deploy \
	predeploy deploy-local deploy-server deploy-phone deploy-all install-hooks

build: ## Build a local (unstamped) binary
	go build -o $(BINARY) ./cmd/gophermind

test: ## Run the full test suite
	go test ./...

predeploy: ## Run the comprehensive pre-deploy test gate (fmt, vet, race tests, iOS)
	./scripts/predeploy.sh

deploy-local: ## Gate, then rebuild the local binary
	./scripts/deploy.sh local

deploy-server: ## Gate, then build + ship to the server
	./scripts/deploy.sh server

deploy-phone: ## Gate, then build + install on the iPhone
	./scripts/deploy.sh phone

deploy-all: ## Gate once, then deploy local + server + phone
	./scripts/deploy.sh all

install-hooks: ## Install the git pre-push hook that runs the gate before every push
	./scripts/install-hooks.sh

ios-test: ## Run the iOS app unit tests on a simulator
	./ios/test.sh

ios-deploy: ## Build + install the iOS app on the connected iPhone
	./ios/deploy.sh

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
	@echo "notarizing the published macOS archive..."
	./scripts/notarize.sh dist/gophermind_*_darwin_all.tar.gz

clean:
	rm -rf dist $(BINARY)
