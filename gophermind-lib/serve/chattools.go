package serve

import (
	"context"

	"gophermind/gophermind-lib/tools"
)

type chatToolsKey struct{}

// withChatTools returns a context carrying the extra tools a chat turn may use.
func withChatTools(ctx context.Context, ts []tools.Tool) context.Context {
	return context.WithValue(ctx, chatToolsKey{}, ts)
}

// ChatToolsFrom returns the chat tools carried by ctx, or nil when the server
// registered none (for example when briefs are not enabled).
func ChatToolsFrom(ctx context.Context) []tools.Tool {
	ts, _ := ctx.Value(chatToolsKey{}).([]tools.Tool)
	return ts
}

// chatTools is the set of brief tools offered to the web chat agent.
type chatTools struct {
	bm *briefManager
	h  *harness // may be nil
}

// all returns the tools. Empty until the brief tools are added.
func (c *chatTools) all() []tools.Tool {
	return []tools.Tool{}
}
