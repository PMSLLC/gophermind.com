package router

import (
	"context"
	"errors"

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

		// Retry the same model, told what was wrong. A chain with nowhere else
		// to go gets the bounded second retry; otherwise one retry, then move on.
		retries := 1
		if r.soleUsable(info) {
			retries = MaxParseRetries
		}
		lastReply, lastErr := res.Text, perr
		for i := 0; i < retries; i++ {
			again := next
			again.Only = res.Entry
			res2, err := r.Call(ctx, again, withParseError(req, lastReply, lastErr))
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
			lastReply, lastErr = res2.Text, perr2
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
