package main

// libui-ng (like AppKit generally) requires every call, across the entire
// process's lifetime, to happen on one consistent OS thread -- not "some
// thread," but the SAME thread every single call after the first. The
// real running app satisfies this naturally: everything from main()
// happens sequentially on the initial goroutine/OS thread, including the
// blocking uiMain() event loop and every callback it invokes.
//
// go test does not preserve that: each top-level test function runs in
// its own freshly-spawned goroutine (see testing.(*T).Run's "go
// func(){...}()"), and Go does not pin a goroutine to a specific OS
// thread unless it calls runtime.LockOSThread() itself. Under a plain
// `go test` run, sequential tests often land on the same OS thread by
// scheduler happenstance (an idle thread gets reused), so this went
// unnoticed through all of 03-01's tests -- but it's not guaranteed, and
// `go test -race` (which adds enough scheduling pressure to actually
// trigger a migration) crashes reliably: AppKit's own thread-consistency
// assertion aborts the process with "Modifications to the layout engine
// must not be performed from a background thread after it has been
// accessed from the main thread."
//
// The fix: one dedicated goroutine, created once for the whole test
// binary, locks itself to its OS thread and services a queue of work
// closures. Every test in this package that touches libui-ng (directly
// or via App/ChatWindow) runs its body through runOnUIThread instead of
// inline, so every libui-ng call across the entire suite -- not just
// within one test -- happens on that same single thread.

import (
	"runtime"
	"sync"
	"testing"
)

var (
	uiThreadOnce sync.Once
	uiThreadWork chan func()
)

func startUIThread() {
	uiThreadWork = make(chan func())
	go func() {
		runtime.LockOSThread()
		for f := range uiThreadWork {
			f()
		}
	}()
}

// runOnUIThread runs f on this test binary's single dedicated,
// OS-thread-locked goroutine and blocks until f returns.
func runOnUIThread(t *testing.T, f func()) {
	t.Helper()
	uiThreadOnce.Do(startUIThread)
	done := make(chan struct{})
	uiThreadWork <- func() {
		defer close(done)
		f()
	}
	<-done
}
