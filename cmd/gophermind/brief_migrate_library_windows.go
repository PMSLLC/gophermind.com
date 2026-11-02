//go:build windows

package main

import "errors"

// freeBytes is not implemented on Windows; the caller says so and goes on.
func freeBytes(path string) (uint64, error) {
	return 0, errors.New("not implemented on Windows")
}
