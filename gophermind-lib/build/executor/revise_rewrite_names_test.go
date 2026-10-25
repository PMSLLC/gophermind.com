package executor

import (
	"reflect"
	"testing"

	"gophermind/gophermind-lib/build/packer"
)

func TestFailedTestNames(t *testing.T) {
	history := []string{
		"attempt 1 cline/m fail test_fail (test_fail: TestSummarizeFile/error_invalid, TestSummarizeFile/happy_stdin)",
		"attempt 2 cline/m fail test_fail (test_fail: TestSummarizeFile/happy_stdin)",
		"attempt 3 mini/m fail build",
	}
	got := failedTestNames(packer.Failure{Names: []string{"TestOther"}}, history)
	want := []string{"TestOther", "TestSummarizeFile/error_invalid", "TestSummarizeFile/happy_stdin"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("failedTestNames = %v, want %v", got, want)
	}
	if got := failedTestNames(packer.Failure{}, []string{"attempt 1 x/y fail build"}); len(got) != 0 {
		t.Fatalf("build failure gave names %v", got)
	}
}
