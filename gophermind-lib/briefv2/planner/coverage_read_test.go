package planner_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
)

func TestReadCoverageMissingIsErrNoCoverage(t *testing.T) {
	dir := t.TempDir()
	_, err := planner.ReadCoverage(dir)
	if !errors.Is(err, planner.ErrNoCoverage) {
		t.Fatalf("missing file: err = %v, want ErrNoCoverage", err)
	}
}

func TestReadCoverageCorruptIsNotErrNoCoverage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "coverage.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := planner.ReadCoverage(dir)
	if err == nil || errors.Is(err, planner.ErrNoCoverage) {
		t.Fatalf("corrupt file: err = %v, want a non-sentinel error", err)
	}
}
