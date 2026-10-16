//go:build !race

package ledger_test

// raceEnabled says the test binary was built with -race (ruling C18).
const raceEnabled = false
