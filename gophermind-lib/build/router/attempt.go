package router

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gophermind/gophermind-lib/build/events"
	"gophermind/gophermind-lib/build/ledger"
	"gophermind/gophermind-lib/build/provider"
)

func (r *Router) acquire(ctx context.Context, provName string) error {
	select {
	case r.slots[provName] <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Router) release(provName string) { <-r.slots[provName] }

// attempt asks one chain entry, retrying transient failures with backoff. It
// returns answered=true with the result, or answered=false with the reason the
// entry did not answer. A non-nil error means the caller's context ended.
func (r *Router) attempt(ctx context.Context, info CallInfo, req provider.Request, entry string, pos int,
	provName, model string, pd promptDigest) (res Result, reason EntryReason, answered bool, err error) {

	p := r.providers[provName]
	backoff := time.Duration(r.cfg.RateLimits.BackoffInitialSeconds) * time.Second
	maxBackoff := time.Duration(r.cfg.RateLimits.BackoffMaxSeconds) * time.Second
	mult := time.Duration(r.cfg.RateLimits.BackoffMultiplier)

	grew := false
	for try := 0; ; try++ {
		if err := r.acquire(ctx, provName); err != nil {
			return Result{}, EntryReason{}, false, err
		}
		callTimeout := r.cfg.CallTimeoutFor(provName)
		callCtx, cancel := context.WithTimeout(ctx, callTimeout)
		r2 := req
		r2.Model = model
		start := r.now()
		resp, cerr := p.Complete(callCtx, r2)
		cancel()
		r.release(provName)

		row := r.newRow(info, pos, provName, model, pd, ledger.OutcomeOK, "")
		row.DurationMS = r.now().Sub(start).Milliseconds()

		if cerr == nil {
			r.answeredOK(provName)
			row.ModelServed = resp.Model
			row.PromptTokens, row.CompletionTokens = resp.Usage.PromptTokens, resp.Usage.CompletionTokens
			row.ResponseBytes, row.ResponseSHA256 = ledger.Digest([]byte(resp.Text))
			if resp.Duration > 0 {
				row.DurationMS = resp.Duration.Milliseconds()
			}
			r.record(ctx, row)
			r.recordText(ctx, row, r2.Messages, resp.Text)
			return Result{Response: resp, CallID: row.ID, Entry: entry, ChainPos: pos}, EntryReason{}, true, nil
		}

		// The caller gave up: record it and stop the whole walk.
		if ctx.Err() != nil {
			row.Outcome, row.ErrorKind, row.ErrorDetail = ledger.OutcomeError, "cancelled", "the run was stopped before the model answered"
			r.record(ctx, row)
			return Result{}, EntryReason{}, false, ctx.Err()
		}

		var rl provider.ErrRateLimited
		var tl provider.ErrContextTooLong
		var au provider.ErrAuth
		var nf provider.ErrModelNotFound
		var tr provider.ErrTruncated
		var er provider.ErrEmptyReply
		switch {
		case errors.As(cerr, &tr):
			row.Outcome, row.ErrorKind = ledger.OutcomeError, "truncated"
			row.ErrorDetail = fmt.Sprintf("the reply used its whole budget of %d tokens before it finished (a reasoning model may be thinking too long)", req.MaxTokens)
			r.record(ctx, row)
			// The model spent its whole budget: try once more on this entry with double.
			if bigger := grownBudget(req.MaxTokens, req.MaxGrownTokens); !grew && bigger > req.MaxTokens {
				grew = true
				req.MaxTokens = bigger
				try--
				continue
			}
			return Result{}, EntryReason{Entry: entry, Kind: ReasonFailed, Detail: TruncatedDetail}, false, nil

		case errors.As(cerr, &er):
			row.Outcome, row.ErrorKind, row.ErrorDetail = ledger.OutcomeError, "empty_reply", "the model returned no text"
			r.record(ctx, row)
			return Result{}, EntryReason{Entry: entry, Kind: ReasonFailed, Detail: "empty reply"}, false, nil

		case errors.As(cerr, &rl):
			d := rl.RetryAfter
			if d <= 0 {
				d = r.defaultCooldown()
			}
			d = r.rateLimited(provName, d)
			row.Outcome, row.ErrorKind, row.RetryAfterS = ledger.OutcomeRateLimited, "rate_limited", int(d/time.Second)
			row.ErrorDetail = "the provider asked for a pause of " + d.String()
			r.record(ctx, row)
			r.setCooldown(provName, d)
			return Result{}, EntryReason{Entry: entry, Kind: ReasonCooldown, Detail: "rate limited for " + d.String()}, false, nil

		case errors.As(cerr, &tl):
			row.Outcome, row.ErrorKind, row.ErrorDetail = ledger.OutcomeTooLong, "too_long", "the provider refused the prompt as too long for the model"
			r.record(ctx, row)
			return Result{}, EntryReason{Entry: entry, Kind: ReasonTooLong, Detail: "the provider refused the prompt as too long"}, false, nil

		case errors.As(cerr, &au):
			row.Outcome, row.ErrorKind, row.ErrorDetail = ledger.OutcomeAuth, "auth", "the provider rejected the credentials"
			r.record(ctx, row)
			r.mu.Lock()
			first := !r.disabled[provName]
			r.disabled[provName] = true
			r.mu.Unlock()
			if first {
				r.emit(events.Event{Kind: events.KindWarning, Stage: info.Stage, NodeID: info.NodeID,
					Message: fmt.Sprintf("provider %s rejected its credentials; it is disabled for this run", provName)})
			}
			return Result{}, EntryReason{Entry: entry, Kind: ReasonAuth, Detail: "authentication failed"}, false, nil

		case errors.As(cerr, &nf):
			row.Outcome, row.ErrorKind, row.ErrorDetail = ledger.OutcomeModelMissing, "model_missing", "the provider does not have this model"
			r.record(ctx, row)
			r.mu.Lock()
			r.missing[entry] = true
			r.mu.Unlock()
			r.emit(events.Event{Kind: events.KindWarning, Stage: info.Stage, NodeID: info.NodeID,
				Message: fmt.Sprintf("model %s was not found on its provider; skipping it for this run", entry)})
			return Result{}, EntryReason{Entry: entry, Kind: ReasonModelMissing}, false, nil

		case errors.Is(cerr, context.DeadlineExceeded):
			// Our own call_timeout expired (the caller's context is still live).
			// The entry sits out a cooldown, so the next call does not wait out another
			// call_timeout on a dead provider; the walk goes on to the next entry.
			row.Outcome, row.ErrorKind, row.ErrorDetail = ledger.OutcomeTimeout, "timeout", "no answer within "+callTimeout.String()
			r.record(ctx, row)
			r.setCooldown(entry, r.timeoutCooldown())
			return Result{}, EntryReason{Entry: entry, Kind: ReasonFailed, Detail: "timed out after " + callTimeout.String()}, false, nil
		}

		// ErrTransient, and anything else the provider returned: retry with backoff.
		row.Outcome, row.ErrorKind, row.ErrorDetail = ledger.OutcomeError, "transient", detailOf(cerr)
		r.record(ctx, row)
		if try >= transientRetries {
			r.setCooldown(provName, r.defaultCooldown())
			return Result{}, EntryReason{Entry: entry, Kind: ReasonCooldown, Detail: fmt.Sprintf("failed %d times in a row", try+1)}, false, nil
		}
		if err := r.sleep(ctx, backoff); err != nil {
			return Result{}, EntryReason{}, false, err
		}
		backoff *= mult
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// maxGrownTokens caps the doubled budget after a truncated reply.
const maxGrownTokens = 16384

// grownBudget doubles a token budget once, capped at limit (the request's own
// cap when positive, else maxGrownTokens); an unset budget becomes 8192.
func grownBudget(n, limit int) int {
	if limit <= 0 {
		limit = maxGrownTokens
	}
	g := n * 2
	if n <= 0 {
		g = 8192
	}
	if g > limit {
		g = limit
	}
	return g
}

// maxDetail bounds the error message kept with a failed call.
const maxDetail = 300

// detailOf is err's message as one bounded line: the provider's own text, with
// white space collapsed, so a failed call can say what went wrong without a
// multi-line body filling the ledger.
func detailOf(err error) string {
	s := strings.Join(strings.Fields(err.Error()), " ")
	if r := []rune(s); len(r) > maxDetail {
		s = string(r[:maxDetail]) + "..."
	}
	return s
}

// recordText hands a recorded call's prompt and reply to a ledger that keeps
// text (a verbose run). A failure is reported as a ledger error and never
// fails the call.
func (r *Router) recordText(ctx context.Context, row *ledger.Call, msgs []provider.Message, reply string) {
	tr, ok := r.led.(ledger.TextRecorder)
	if !ok || row.ID == 0 {
		return
	}
	t := ledger.Text{ID: row.ID, Stage: row.Stage, NodeID: row.NodeID, Provider: row.Provider, Model: row.ModelServed, Response: reply}
	for _, m := range msgs {
		t.Messages = append(t.Messages, ledger.TextMessage{Role: string(m.Role), Content: m.Content})
	}
	if err := tr.RecordText(context.WithoutCancel(ctx), row.RunID, t); err != nil {
		r.noteLedgerError(row.Stage, row.NodeID, err)
	}
}
