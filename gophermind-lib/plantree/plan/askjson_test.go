package plan

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func parseNumber(reply string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(reply))
	if err != nil {
		return 0, fmt.Errorf("not a number: %q", reply)
	}
	return n, nil
}

func TestAskJSONReturnsAGoodFirstReply(t *testing.T) {
	f := &fake{reply: func(int, string) (string, error) { return "7", nil }}
	n, err := askJSON(context.Background(), f, "PROMPT", parseNumber)
	if err != nil || n != 7 || len(f.prompts) != 1 {
		t.Errorf("n=%d err=%v calls=%d", n, err, len(f.prompts))
	}
}

func TestAskJSONRetriesOnceShowingTheBadReply(t *testing.T) {
	f := &fake{reply: func(n int, _ string) (string, error) {
		if n == 0 {
			return "seven", nil
		}
		return "7", nil
	}}
	n, err := askJSON(context.Background(), f, "PROMPT", parseNumber)
	if err != nil || n != 7 || len(f.prompts) != 2 {
		t.Fatalf("n=%d err=%v calls=%d", n, err, len(f.prompts))
	}
	if !strings.Contains(f.prompts[1], "PROMPT") || !strings.Contains(f.prompts[1], "rejected") || !strings.Contains(f.prompts[1], "seven") {
		t.Errorf("the retry prompt must carry the original, the reason and the bad reply: %q", f.prompts[1])
	}
}

func TestAskJSONGivesUpAfterTwoRejections(t *testing.T) {
	f := &fake{reply: func(int, string) (string, error) { return "nope", nil }}
	_, err := askJSON(context.Background(), f, "PROMPT", parseNumber)
	if err == nil || !strings.Contains(err.Error(), "rejected twice") || len(f.prompts) != 2 {
		t.Errorf("err=%v calls=%d", err, len(f.prompts))
	}
}

func TestAskJSONReturnsTransportErrorsUnchanged(t *testing.T) {
	boom := errors.New("connection refused")
	f := &fake{reply: func(int, string) (string, error) { return "", boom }}
	if _, err := askJSON(context.Background(), f, "P", parseNumber); !errors.Is(err, boom) || len(f.prompts) != 1 {
		t.Errorf("first call: err=%v calls=%d", err, len(f.prompts))
	}
	f2 := &fake{reply: func(n int, _ string) (string, error) {
		if n == 0 {
			return "bad", nil
		}
		return "", boom
	}}
	if _, err := askJSON(context.Background(), f2, "P", parseNumber); !errors.Is(err, boom) || len(f2.prompts) != 2 {
		t.Errorf("retry call: err=%v calls=%d", err, len(f2.prompts))
	}
}
