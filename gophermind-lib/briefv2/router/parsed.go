package router

import (
	"context"
	"errors"

	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/provider"
)

// CallParsed is Call plus the stage's parser. A reply that arrives fine but
// does not parse is a failed attempt: its ledger row is corrected to
// malformed, the same model gets one more try with the parse error appended,
// and if that also fails the model is excluded and the chain continues.
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

		// One more try on the same model, told what was wrong.
		again := next
		again.Only = res.Entry
		res2, err := r.Call(ctx, again, withParseError(req, res.Text, perr))
		if err == nil {
			perr2 := parse(res2.Text)
			if perr2 == nil {
				return res2, nil
			}
			lastParse = perr2
			r.markMalformed(ctx, info.RunID, res2)
		} else {
			var ce *ChainExhausted
			if !errors.As(err, &ce) {
				return Result{}, err // the caller's context ended
			}
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
