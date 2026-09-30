package planner

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// answer is one entry of answers.json: a question a model asked and what the
// owner said, or what was assumed when nobody was asked.
type answer struct {
	ID       string `json:"id"`
	Stage    string `json:"stage"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
	Assumed  bool   `json:"assumed"`
}

type answersFile struct {
	Answers []answer `json:"answers"`
}

func loadAnswers(r *run) (answersFile, error) {
	as := answersFile{Answers: []answer{}}
	_, err := readJSON(r.path(fileAnswers), &as)
	return as, err
}

// answersText is how earlier answers are shown to a model.
func answersText(as answersFile) string {
	if len(as.Answers) == 0 {
		return "(no questions were asked)"
	}
	var b strings.Builder
	for _, a := range as.Answers {
		fmt.Fprintf(&b, "Q: %s\nA: %s", a.Question, a.Answer)
		if a.Assumed {
			b.WriteString(" (assumed, nobody was asked)")
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func removeFile(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
