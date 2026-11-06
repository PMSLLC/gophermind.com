package router_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"gophermind/gophermind-lib/build/provider"
	"gophermind/gophermind-lib/build/router"
)

// kindErr is a parse error that names its own kind.
type kindErr struct{}

func (kindErr) Error() string     { return "contract: the reply quotes secret text" }
func (kindErr) ErrorKind() string { return "fixed_interface_missing" }

var _ router.Kinder = kindErr{}

func TestMalformedRowNamesItsKind(t *testing.T) {
	cases := []struct {
		name string
		perr error
		want string
	}{
		{"contract error", fmt.Errorf("contract: function f1 is not declared"), "contract"},
		{"conflicting declaration", fmt.Errorf("contract: conflicting declaration of f1 by component c"), "conflict"},
		{"plain parse error", errors.New("bad json"), "malformed"},
		{"error that names its kind", fmt.Errorf("wrapped: %w", kindErr{}), "fixed_interface_missing"},
		{"schema error", errors.New("schema: field x is missing"), "schema"},
		{"unknown first word", errors.New("stage contract:x did not finish"), "malformed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newRig(t, func(call int, r provider.Request) (provider.Response, error) {
				if call == 1 {
					return provider.Response{Text: "bad", Model: r.Model}, nil
				}
				return provider.Response{Text: "good", Model: r.Model}, nil
			}, nil, nil, nil)
			parse := func(text string) error {
				if text == "bad" {
					return c.perr
				}
				return nil
			}
			if _, err := g.r.CallParsed(context.Background(), info(router.TierStandard, router.ScopeNode), req("write JSON"), parse); err != nil {
				t.Fatal(err)
			}
			rows := g.rows(t)
			if len(rows) != 2 || rows[0].ErrorKind != c.want {
				t.Errorf("rows = %+v, want the first row's error kind %q", rows, c.want)
			}
		})
	}
}
