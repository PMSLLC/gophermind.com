//go:build windows

package provider

import (
	"context"
	"errors"
	"time"
)

// ClineConfig mirrors the Unix build's config; the provider itself is not
// available on Windows.
type ClineConfig struct {
	Name          string
	Binary        string
	ClineProvider string
	SettingsDir   string
	Timeout       time.Duration
	MaxConcurrent int
	Models        []ModelInfo
	MaxCopyBytes  int64
	MaxCopyFiles  int
}

// Cline is a stub on Windows: Complete always fails.
type Cline struct{ cfg ClineConfig }

var _ Provider = (*Cline)(nil)

func NewCline(cfg ClineConfig) *Cline { return &Cline{cfg: cfg} }

func (c *Cline) Name() string        { return c.cfg.Name }
func (c *Cline) Models() []ModelInfo { return c.cfg.Models }

func (c *Cline) Complete(ctx context.Context, req Request) (Response, error) {
	return Response{}, errors.New("provider: the Cline provider is not available on Windows")
}
