//go:build !unix

package intro

// Play is a no-op on non-unix platforms. The intro relies on raw, non-blocking
// terminal I/O that this package only implements for unix; Windows and other
// targets simply skip straight to the TUI.
func Play() {}
