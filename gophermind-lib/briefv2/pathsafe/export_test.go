package pathsafe

// SetBeforeRename installs a seam that runs between the last parent check
// and the rename; it returns a restore function.
func SetBeforeRename(f func()) func() {
	old := beforeRename
	beforeRename = f
	return func() { beforeRename = old }
}

// SetAfterRename installs a seam that runs right after the rename.
func SetAfterRename(f func()) func() {
	old := afterRename
	afterRename = f
	return func() { afterRename = old }
}
