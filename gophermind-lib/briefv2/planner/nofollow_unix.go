//go:build !windows

package planner

import "syscall"

// openNoFollow makes the final path component of an open refuse a symbolic link.
const openNoFollow = syscall.O_NOFOLLOW
