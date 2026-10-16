//go:build windows

package envcheck

import "errors"

func freeBytes(path string) (uint64, error) {
	return 0, errors.New("free space is not measured on this platform")
}
