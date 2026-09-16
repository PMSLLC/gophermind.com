//go:build darwin && amd64

package main

// Package-wide cgo flags for libui-ng and AppKit, Intel paths.
//
// cgo gathers every #cgo directive in a package and applies the union to all
// of that package's files, so one declaration covers the whole package. These
// lines used to be copy-pasted into each of the nine cgo files, which is why
// every link printed "ld: warning: ignoring duplicate libraries: '-lui'".
//
// The paths differ by architecture -- libui-ng's meson install lands in
// /opt/homebrew on Apple Silicon and /usr/local on Intel (see README.md) --
// so they live in a pair of GOARCH-tagged files rather than one hardcoded
// prefix. libui-ng ships no pkg-config file, so there is nothing to query.
//
// -x objective-c and -framework AppKit were declared in chatinput.go. By the
// accumulation rule above they have always applied to every cgo preamble in
// the package; stating them here makes that visible instead of implicit.

/*
#cgo CFLAGS: -I/usr/local/include -x objective-c
#cgo LDFLAGS: -L/usr/local/lib -lui -framework AppKit
*/
import "C"
