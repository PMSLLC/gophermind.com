package router

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/provider"
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
		callCtx, cancel := context.WithTimeout(ctx, r.cfg.Defaults.CallTimeout)
		r2 := req
		r2.Model = model
		start := r.now()
		resp, cerr := p.Complete(callCtx, r2)
		cancel()
		r.release(provName)

		row := r.newRow(info, pos, provName, model, pd, ledger.OutcomeOK, "")
		row.DurationMS = r.now().Sub(start).Milliseconds()

		if cerr == nil {
			row.ModelServed = resp.Model
			row.PromptTokens, row.CompletionTokens = resp.Usage.PromptTokens, resp.Usage.CompletionTokens
			row.ResponseBytes, row.ResponseSHA256 = ledger.Digest([]byte(resp.Text))
			if resp.Duration > 0 {
				row.DurationMS = resp.Duration.Milliseconds()
			}
			r.record(ctx, row)
			return Result{Response: resp, CallID: row.ID, Entry: entry, ChainPos: pos}, EntryReason{}, true, nil
		}

		// The caller gave up: record it and stop the whole walk.
		if ctx.Err() != nil {
			row.Outcome, row.ErrorKind = ledger.OutcomeError, "cancelled"
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
			r.record(ctx, row)
			// The model spent its whole budget: try once more on this entry with double.
			if bigger := grownBudget(req.MaxTokens, req.MaxGrownTokens); !grew && bigger > req.MaxTokens {
				grew = true
				req.MaxTokens = bigger
				try--
				continue
			}
			return Result{}, EntryReason{Entry: entry, Kind: ReasonFailed, Detail: "reply truncated at the token limit"}, false, nil

		case errors.As(cerr, &er):
			row.Outcome, row.ErrorKind = ledger.OutcomeError, "empty_reply"
			r.record(ctx, row)
			return Result{}, EntryReason{Entry: entry, Kind: ReasonFailed, Detail: "empty reply"}, false, nil

		case errors.As(cerr, &rl):
			d := rl.RetryAfter
			if d <= 0 {
				d = r.defaultCooldown()
			}
			row.Outcome, row.ErrorKind, row.RetryAfterS = ledger.OutcomeRateLimited, "rate_limited", int(d/time.Second)
			r.record(ctx, row)
			r.setCooldown(provName, d)
			return Result{}, EntryReason{Entry: entry, Kind: ReasonCooldown, Detail: "rate limited for " + d.String()}, false, nil

		case errors.As(cerr, &tl):
			row.Outcome, row.ErrorKind = ledger.OutcomeTooLong, "too_long"
			r.record(ctx, row)
			return Result{}, EntryReason{Entry: entry, Kind: ReasonTooLong, Detail: "the provider refused the prompt as too long"}, false, nil

		case errors.As(cerr, &au):
			row.Outcome, row.ErrorKind = ledger.OutcomeAuth, "auth"
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
			row.Outcome, row.ErrorKind = ledger.OutcomeModelMissing, "model_missing"
			r.record(ctx, row)
			r.mu.Lock()
			r.missing[entry] = true
			r.mu.Unlock()
			r.emit(events.Event{Kind: events.KindWarning, Stage: info.Stage, NodeID: info.NodeID,
				Message: fmt.Sprintf("model %s was not found on its provider; skipping it for this run", entry)})
			return Result{}, EntryReason{Entry: entry, Kind: ReasonModelMissing}, false, nil

		case errors.Is(cerr, context.DeadlineExceeded):
			// Our own call_timeout expired (the caller's context is still live).
			row.Outcome, row.ErrorKind = ledger.OutcomeTimeout, "timeout"
			r.record(ctx, row)
			return Result{}, EntryReason{Entry: entry, Kind: ReasonFailed, Detail: "timed out after " + r.cfg.Defaults.CallTimeout.String()}, false, nil
		}

		// ErrTransient, and anything else the provider returned: retry with backoff.
		row.Outcome, row.ErrorKind = ledger.OutcomeError, "transient"
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
