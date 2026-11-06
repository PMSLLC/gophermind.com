package router

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"unicode/utf8"

	"gophermind/gophermind-lib/build/ledger"
	"gophermind/gophermind-lib/build/provider"
	"gophermind/gophermind-lib/build/settings"
)

// MaxParseRetries is how many told-what-was-wrong retries the same model gets
// when it is the only usable entry in the tier chain. With other entries to
// fall back on, a model gets one.
const MaxParseRetries = 2

// CallParsed is Call plus the stage's parser. A reply that arrives fine but
// does not parse is a failed attempt: its ledger row is corrected to
// malformed, the same model gets one more try with the parse error appended
// (up to MaxParseRetries when it is the chain's only usable entry), and if
// that also fails the model is excluded and the chain continues.
func (r *Router) CallParsed(ctx context.Context, info CallInfo, req provider.Request, parse func(text string) error) (Result, error) {
	exclude := append([]string(nil), info.Exclude...)
	var lastParse error
	seen := map[[32]byte]bool{} // hashes of replies that already failed the parser

	for {
		next := info
		next.Exclude = exclude
		res, err := r.Call(ctx, next, req)
		if err != nil {
			return Result{}, withParseErr(err, lastParse)
		}
		perr := parse(res.Text)
		if perr == nil {
			return res, nil
		}
		lastParse = perr
		r.markMalformed(ctx, info.RunID, res)
		if errors.Is(perr, ErrNoRetry) {
			return Result{}, perr
		}
		var errs []string
		dup := noteReply(seen, res.Text)
		errs = addErr(errs, perr, dup)

		// Retry the same model, told what was wrong. A chain with nowhere else
		// to go gets the bounded second retry; otherwise one retry, then move on.
		retries := 1
		if r.soleUsable(info) {
			retries = MaxParseRetries
			if n := r.cfg.Defaults.MaxParseRetries; n > 0 {
				retries = n
			}
		}
		lastReply, lastErr := res.Text, perr
		for i := 0; i < retries; i++ {
			again := next
			again.Only = res.Entry
			// Retry 1 is conversational. Later retries, and any retry after a
			// repeated reply, drop the replies and carry the error list instead,
			// so a small model has nothing to copy.
			retryReq := withParseError(req, lastReply, lastErr)
			if i > 0 || dup {
				retryReq = withErrorNote(req, errs)
			}
			res2, err := r.Call(ctx, again, retryReq)
			if err != nil {
				var ce *ChainExhausted
				if !errors.As(err, &ce) {
					return Result{}, err // the caller's context ended
				}
				break
			}
			perr2 := parse(res2.Text)
			if perr2 == nil {
				return res2, nil
			}
			lastParse = perr2
			r.markMalformed(ctx, info.RunID, res2)
			if errors.Is(perr2, ErrNoRetry) {
				return Result{}, perr2
			}
			lastReply, lastErr = res2.Text, perr2
			dup = noteReply(seen, res2.Text)
			errs = addErr(errs, perr2, dup)
		}
		exclude = append(exclude, res.Entry)
	}
}

func (r *Router) markMalformed(ctx context.Context, runID string, res Result) {
	if r.led == nil || res.CallID == 0 {
		return
	}
	// The error kind is a fixed word: a parser error can quote the reply, and reply text is never stored.
	if err := r.led.Amend(context.WithoutCancel(ctx), runID, res.CallID, ledger.OutcomeMalformed, "malformed"); err != nil {
		r.noteLedgerError("", "", err)
	}
}

func withParseErr(err error, parseErr error) error {
	var ce *ChainExhausted
	if parseErr != nil && errors.As(err, &ce) {
		ce.ParseErr = parseErr
	}
	return err
}

func withParseError(req provider.Request, reply string, perr error) provider.Request {
	msgs := append([]provider.Message(nil), req.Messages...)
	msgs = append(msgs,
		provider.Message{Role: provider.RoleAssistant, Content: reply},
		provider.Message{Role: provider.RoleUser, Content: "That reply could not be used: " + perr.Error() + "\nReply again with the corrected output only."})
	req.Messages = msgs
	return req
}

// noteReply records the reply's hash and reports whether it was seen before.
func noteReply(seen map[[32]byte]bool, text string) bool {
	h := sha256.Sum256([]byte(text))
	was := seen[h]
	seen[h] = true
	return was
}

// addErr appends the parse error (and, for a repeated reply, a note saying so)
// to the list, keeping each message once.
func addErr(errs []string, perr error, dup bool) []string {
	add := func(msg string) {
		msg = strings.Join(strings.Fields(msg), " ")
		for _, e := range errs {
			if e == msg {
				return
			}
		}
		errs = append(errs, msg)
	}
	add(perr.Error())
	if dup {
		add("the reply was identical to an earlier one")
	}
	return errs
}

const errorNoteCap = 600

// withErrorNote is the fresh retry form: the original messages plus one user
// note listing the distinct errors so far. The note is at most errorNoteCap
// bytes; whole lines are kept and the oldest are dropped first. A single line
// too long on its own is cut.
func withErrorNote(req provider.Request, errs []string) provider.Request {
	const head = "Earlier attempts could not be used. Do not repeat them. Errors so far:"
	lines := make([]string, len(errs))
	for i, e := range errs {
		lines[i] = "\n- " + e
	}
	budget := errorNoteCap - len(head)
	start, used := len(lines), 0
	for start > 0 && used+len(lines[start-1]) <= budget {
		start--
		used += len(lines[start])
	}
	note := head + strings.Join(lines[start:], "")
	if start == len(lines) && len(lines) > 0 {
		last := lines[len(lines)-1]
		cut := budget
		for cut > 0 && !utf8.RuneStart(last[cut]) {
			cut--
		}
		note = head + last[:cut]
	}
	req.Messages = append(append([]provider.Message(nil), req.Messages...),
		provider.Message{Role: provider.RoleUser, Content: note})
	return req
}

// soleUsable reports whether the call's tier chain has exactly one entry that
// is not excluded by the caller or gated off (no provider, auth failure,
// privacy, cooldown).
func (r *Router) soleUsable(info CallInfo) bool {
	chain, err := r.chainFor(info)
	if err != nil {
		return false
	}
	usable := 0
	for _, entry := range chain {
		provName, _, _ := settings.SplitEntry(entry)
		if _, skip := r.gate(info, entry, provName); !skip {
			usable++
		}
	}
	return usable == 1
}
