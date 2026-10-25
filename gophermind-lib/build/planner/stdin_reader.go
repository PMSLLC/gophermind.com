package planner

import (
	"errors"
	"regexp"
	"strings"

	"gophermind/gophermind-lib/build/contract"
)

var (
	stdinDocRE     = regexp.MustCompile(`(?i)\bstdin\b|\bstandard input\b`)
	readerParamRE  = regexp.MustCompile(`\bio\.(Reader|ReadCloser|ReadSeeker)\b|\bbufio\.(Reader|Scanner)\b|\*os\.File\b`)
	errStdinReader = errors.New("a function whose doc says it reads standard input must take an io.Reader parameter; choose the input (a file or os.Stdin) in the entry point and pass it in, so a test can pass strings.NewReader")
)

// stdinNeedsReader refuses a function whose doc says it reads standard input
// while its signature takes no reader. Such a function can only be tested by
// replacing os.Stdin, which the test-writer keeps getting wrong; the entry
// point (main) is where os.Stdin is chosen, so it is exempt. The check reads
// the signature and doc only, so a body that reads stdin unannounced is not
// caught here. The error never quotes the function.
func stdinNeedsReader(f contract.Function) error {
	if strings.HasPrefix(strings.TrimSpace(f.Signature), "func main(") {
		return nil
	}
	if !stdinDocRE.MatchString(f.Doc) || readerParamRE.MatchString(f.Signature) {
		return nil
	}
	return errStdinReader
}
