package bubblecomplete

import (
	"testing"
)

// staticProvider is a trivial Provider implementation for testing.
type staticProvider struct {
	name       string
	candidates []Candidate
}

func (p *staticProvider) Name() string {
	return p.name
}

func (p *staticProvider) Suggest(input string, cursor int) []Candidate {
	return p.candidates
}

// Verify at compile-time that staticProvider satisfies Provider interface.
var _ Provider = (*staticProvider)(nil)

func TestProviderInterface(t *testing.T) {
	tests := []struct {
		name      string
		provider  *staticProvider
		input     string
		cursor    int
		wantName  string
		wantCount int
		wantFirst *Candidate
	}{
		{
			name: "empty input returns candidates",
			provider: &staticProvider{
				name: "test-provider",
				candidates: []Candidate{
					{Text: "hello", Display: "hello", Desc: "greeting", Replace: 0},
					{Text: "world", Display: "world", Desc: "noun", Replace: 0},
				},
			},
			input:     "",
			cursor:    0,
			wantName:  "test-provider",
			wantCount: 2,
			wantFirst: &Candidate{Text: "hello", Display: "hello", Desc: "greeting", Replace: 0},
		},
		{
			name: "with cursor position",
			provider: &staticProvider{
				name: "another-provider",
				candidates: []Candidate{
					{Text: "foo", Display: "foo-display", Desc: "foo description", Replace: 3},
				},
			},
			input:     "foo",
			cursor:    3,
			wantName:  "another-provider",
			wantCount: 1,
			wantFirst: &Candidate{Text: "foo", Display: "foo-display", Desc: "foo description", Replace: 3},
		},
		{
			name: "no candidates",
			provider: &staticProvider{
				name:       "empty-provider",
				candidates: []Candidate{},
			},
			input:     "xyz",
			cursor:    3,
			wantName:  "empty-provider",
			wantCount: 0,
			wantFirst: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test Name()
			if got := tt.provider.Name(); got != tt.wantName {
				t.Errorf("Name() = %q, want %q", got, tt.wantName)
			}

			// Test Suggest()
			got := tt.provider.Suggest(tt.input, tt.cursor)
			if len(got) != tt.wantCount {
				t.Errorf("Suggest() returned %d candidates, want %d", len(got), tt.wantCount)
			}

			// Test first candidate fields
			if tt.wantFirst != nil && len(got) > 0 {
				if got[0].Text != tt.wantFirst.Text {
					t.Errorf("Candidate.Text = %q, want %q", got[0].Text, tt.wantFirst.Text)
				}
				if got[0].Display != tt.wantFirst.Display {
					t.Errorf("Candidate.Display = %q, want %q", got[0].Display, tt.wantFirst.Display)
				}
				if got[0].Desc != tt.wantFirst.Desc {
					t.Errorf("Candidate.Desc = %q, want %q", got[0].Desc, tt.wantFirst.Desc)
				}
				if got[0].Replace != tt.wantFirst.Replace {
					t.Errorf("Candidate.Replace = %d, want %d", got[0].Replace, tt.wantFirst.Replace)
				}
			}
		})
	}
}
