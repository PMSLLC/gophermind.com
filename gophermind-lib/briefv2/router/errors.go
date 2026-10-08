package router

import (
	"fmt"
	"strings"
)

// Why one chain entry did not answer.
const (
	ReasonExcluded     = "excluded"      // the caller asked to skip it (after a malformed reply)
	ReasonNoProvider   = "no_provider"   // the entry names a provider that was not built
	ReasonModelMissing = "model_missing" // the provider said the model does not exist
	ReasonAuth         = "auth"          // bad or missing key; disabled for the run
	ReasonPrivacy      = "privacy"       // a public provider may not see this scope
	ReasonCooldown     = "cooldown"      // rate limited or failing; try again later
	ReasonTooLong      = "too_long"      // the prompt does not fit the model
	ReasonFailed       = "failed"        // the call was made and failed (for example a timeout)
)

// TruncatedDetail is the Detail of a ReasonFailed row whose reply was cut off at
// the token limit, even after one retry with a doubled budget.
const TruncatedDetail = "reply truncated at the token limit"

// EntryReason is one row of a ChainExhausted report.
type EntryReason struct {
	Entry  string // provider/model
	Kind   string // one of the Reason constants
	Detail string
}

// ChainExhausted is returned when no entry of the tier's chain answered. It
// says why each one did not, so the caller can choose between waiting, asking
// a human, or failing with a message that names the setting to change.
type ChainExhausted struct {
	Tier     Tier
	Reasons  []EntryReason
	ParseErr error // the last parse error, when CallParsed excluded entries for malformed replies
}

func (e *ChainExhausted) Error() string {
	parts := make([]string, 0, len(e.Reasons))
	for _, r := range e.Reasons {
		s := r.Entry + ": " + r.Kind
		if r.Detail != "" {
			s += " (" + r.Detail + ")"
		}
		parts = append(parts, s)
	}
	msg := fmt.Sprintf("router: no model answered for tier %s: %s", e.Tier, strings.Join(parts, "; "))
	if e.ParseErr != nil {
		msg += "; last parse error: " + e.ParseErr.Error()
	}
	return msg
}

// Truncated reports whether an entry failed because its reply was cut off at
// the token limit. A caller that can ask for less at a time may retry smaller.
func (e *ChainExhausted) Truncated() bool {
	for _, r := range e.Reasons {
		if r.Kind == ReasonFailed && r.Detail == TruncatedDetail {
			return true
		}
	}
	return false
}

func (e *ChainExhausted) Unwrap() error { return e.ParseErr }

// OnlyPrivacy reports whether every entry was skipped because a public
// provider may not see this scope. The fix is then --allow-public, a private
// model, or a config edit, not waiting.
func (e *ChainExhausted) OnlyPrivacy() bool {
	if len(e.Reasons) == 0 {
		return false
	}
	for _, r := range e.Reasons {
		if r.Kind != ReasonPrivacy {
			return false
		}
	}
	return true
}

// OnlyTooLong reports whether every entry was skipped or refused because the
// prompt does not fit its model, and no reply was ever seen. The caller can
// then send a smaller prompt instead of giving up or excluding the model.
func (e *ChainExhausted) OnlyTooLong() bool {
	if len(e.Reasons) == 0 || e.ParseErr != nil {
		return false
	}
	for _, r := range e.Reasons {
		if r.Kind != ReasonTooLong {
			return false
		}
	}
	return true
}
