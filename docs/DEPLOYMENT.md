# Deployment

Every deployment runs a comprehensive test gate first and is **fail-closed**:
if the gate does not pass, nothing is built or shipped.

## The gate

`scripts/predeploy.sh` runs, in order (cheapest first):

1. `gofmt` — formatting is clean
2. `go vet ./...`
3. `go build ./...`
4. `go test -race ./...` — the full Go suite under the race detector
5. iOS `XCTest` suite on a simulator (macOS only)

Any failing stage aborts with a non-zero exit. The iOS stage can be skipped
only with `GOPHERMIND_SKIP_IOS=1`, which prints a loud warning — skipping tests
before a deploy is a deliberate, visible choice, never a silent default.

Run it on its own:

```
make predeploy
```

## Deploying

The deploy commands run the gate first and refuse to proceed on red:

```
make deploy-local     # gate, then rebuild ./gophermind
make deploy-server    # gate, then build linux/amd64 + ship to the server
make deploy-phone     # gate, then build + install the iOS app on the iPhone
make deploy-all       # gate once, then all three
```

Or directly: `scripts/deploy.sh {local|server|phone|all}`.

The server deploy checksums the binary after upload, keeps a timestamped backup
of the previous binary, restarts the systemd unit, and verifies `/healthz`
before reporting success. Override the host with `SERVER_HOST=…`.

## Gating pushes too

A push is the upstream of every deployment. Install a pre-push hook that runs
the same gate before any `git push`:

```
make install-hooks
```

Emergency bypass (deliberate and visible): `git push --no-verify`.

## Why fail-closed

The gate is the single choke point where "did the tests pass?" is answered
before code reaches a running system. Making it part of the deploy path — rather
than a step someone remembers to run — is what makes the answer reliable.
