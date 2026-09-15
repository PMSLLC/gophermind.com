# gophermind-osx

Native macOS desktop app for gophermind (see `.planning/ROADMAP.md` Phase 3),
built with [libui-ng](https://github.com/libui-ng/libui-ng) via cgo.

## Build requirement: libui-ng

`libui-ng` is not a Go module and not available via Homebrew — it must be
built from source and installed before `go build` here will work. It is not
vendored into this repo; each machine building `gophermind-osx` needs its own
copy installed.

```bash
brew install meson ninja   # if not already present
git clone --depth 1 https://github.com/libui-ng/libui-ng.git /tmp/libui-ng
cd /tmp/libui-ng
meson setup build -Dexamples=false -Dtests=false --buildtype=release
ninja -C build
meson install -C build     # installs to /opt/homebrew/{lib,include}
rm -rf /tmp/libui-ng
```

`app.go`'s cgo directives point at `/opt/homebrew/{include,lib}` directly
(no `pkg-config` file ships with libui-ng). If your Homebrew prefix differs
(e.g. Intel Mac, `/usr/local`), adjust those `#cgo CFLAGS`/`#cgo LDFLAGS`
paths accordingly.

Why not `github.com/andlabs/ui` (the obvious pre-built Go binding)? Its
bundled darwin static library is amd64-only, dated 2020, predating Apple
Silicon — broken on an arm64 Mac. `app.go` binds directly to libui-ng's C
API instead, for just the surface this app currently needs.

## Building and running

Bare binary (for development):

```bash
cd gophermind-osx
go build -o gophermind-osx .
./gophermind-osx
```

Proper `.app` bundle (dock icon, Finder integration):

```bash
cd gophermind-osx
./build-app.sh
open build/GopherMind.app
```

`go test ./...` does not require a display session or `libui-ng`'s event
loop (`App.Run()`, which calls the blocking `uiMain()`, is intentionally not
exercised by any test — see `app_test.go`'s doc comment). It does require
`libui-ng` to be installed, same as building the binary.
