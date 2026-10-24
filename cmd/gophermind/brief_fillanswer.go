package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"gophermind/gophermind-lib/serve"
)

// briefFillAnswer is `gophermind brief fill-answer <run-id> <question-id>
// --repo <root>`: the answer text comes on stdin and goes into the run's
// QUESTIONS.md. The web server runs it as the run user (through sudo) because
// it may not write that file itself. Not listed in the usage text.
func briefFillAnswer(args []string, in io.Reader, out, errw io.Writer) int {
	if len(args) != 4 || args[2] != "--repo" {
		fmt.Fprintln(errw, "usage: gophermind brief fill-answer <run-id> <question-id> --repo <root> (answer on stdin)")
		return 1
	}
	text, err := io.ReadAll(io.LimitReader(in, 16<<10))
	if err != nil {
		fmt.Fprintln(errw, "gophermind brief fill-answer:", err)
		return 1
	}
	n, err := serve.FillGateAnswer(args[3], args[0], args[1], string(text))
	switch {
	case errors.Is(err, serve.ErrNoSuchQuestion):
		fmt.Fprintln(errw, "gophermind brief fill-answer: no such open question")
		return 3
	case errors.Is(err, os.ErrNotExist):
		fmt.Fprintln(errw, "gophermind brief fill-answer: the run is not waiting for answers")
		return 4
	case err != nil:
		fmt.Fprintln(errw, "gophermind brief fill-answer:", err)
		return 1
	}
	fmt.Fprintf(out, "remaining %d\n", n)
	return 0
}
