//go:build !windows

package pathsafe

import "syscall"

// NoFollow makes the final path component of an open refuse a symbolic link.
const NoFollow = syscall.O_NOFOLLOW
