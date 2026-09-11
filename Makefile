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

# One-command public release: gate, tag, GitHub + Homebrew, then npm — with a
# confirmation before each irreversible step. This is the target to use.
#   make publish VERSION=0.6.0
#   make publish VERSION=0.6.0 DRY_RUN=1
publish: ## Release to GitHub + Homebrew + npm (VERSION=x.y.z [DRY_RUN=1])
	@if [ -z "$(VERSION)" ]; then \
		echo "usage: make publish VERSION=0.6.0 [DRY_RUN=1]" >&2; exit 2; \
	fi
	./scripts/release.sh $(VERSION) $(if $(DRY_RUN),--dry-run,)

# Lower-level: the GoReleaser half only (GitHub + Homebrew, no npm), against a
# tag you have already pushed. `make publish` is the complete path; keep this
# for re-running the build when a release is otherwise already done.
release: ## GoReleaser only — no npm, needs an existing tag
	@: $${MACOS_SIGN_IDENTITY:?set MACOS_SIGN_IDENTITY, e.g. \"Developer ID Application: Your Name (TEAMID)\" — see docs/RELEASING.md}
	@: $${MACOS_NOTARY_PROFILE:?set MACOS_NOTARY_PROFILE to your notarytool keychain profile — see docs/RELEASING.md}
	# Notarization runs INSIDE the goreleaser pipeline now (a universal_binaries
	# post hook), so the binary is notarized before it is archived or uploaded.
	# It used to run here, after publishing, which shipped an un-notarized
	# tarball first and was skipped entirely whenever goreleaser failed at any
	# later publish step.
	GITHUB_TOKEN="$${GITHUB_TOKEN:-$$(gh auth token 2>/dev/null)}" goreleaser release --clean

clean:
	rm -rf dist $(BINARY)
