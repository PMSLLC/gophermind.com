package provider

// ErrModelNotFound: the provider answered 404 "model not found" (the mini's
// Ollama did this on 2026-09-29 when a model was removed mid-run). The router
// skips that provider/model entry for the rest of the run and warns once.
type ErrModelNotFound struct{ Model string }

func (e ErrModelNotFound) Error() string { return "provider: model not found: " + e.Model }

// ErrTruncated: the reply had empty content and finish_reason "length": the
// model spent the whole token budget before answering. The router retries once
// on the same entry with a larger budget. It names the provider only.
type ErrTruncated struct{ Provider string }

func (e ErrTruncated) Error() string {
	return "provider " + e.Provider + ": reply truncated at the token limit with no content"
}

// ErrEmptyReply: the reply had empty content for a reason other than the
// token limit. It names the provider only.
type ErrEmptyReply struct{ Provider string }

func (e ErrEmptyReply) Error() string { return "provider " + e.Provider + ": reply had empty content" }
