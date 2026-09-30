package provider

// ErrModelNotFound: the provider answered 404 "model not found" (the mini's
// Ollama did this on 2026-09-29 when a model was removed mid-run). The router
// skips that provider/model entry for the rest of the run and warns once.
type ErrModelNotFound struct{ Model string }

func (e ErrModelNotFound) Error() string { return "provider: model not found: " + e.Model }
