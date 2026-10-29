package planner

import (
	"strings"
	"testing"
)

// func main() only wires other packages, so a test cannot call the code it
// wires, and a model asked to "call the functions directly" called a function
// of another package without qualifying it, three times in a row. The prompt
// has to say what a test of main can be.
func TestTestwriterPromptSaysWhatATestOfMainIs(t *testing.T) {
	main := `{"id":"fn-main","contract":{"signature":"func main()"}}`
	got, err := renderTestwriterWith(main, "example.com/slugify", "cmd/slugify/main.go", "TestFnMain", "cmd/slugify/fn_main_test.go", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "_ = main") || !strings.Contains(got, "acceptance") {
		t.Error("the prompt for func main() does not say its test only proves it exists (_ = main)")
	}

	other := `{"id":"fn-helper","contract":{"signature":"func helper(s string) string"}}`
	got, err = renderTestwriterWith(other, "example.com/slugify", "cmd/slugify/helper.go", "TestFnHelper", "cmd/slugify/fn_helper_test.go", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "_ = main") {
		t.Error("the main-only instruction leaked into the prompt of another function")
	}
}
