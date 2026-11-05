package planner_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gophermind/gophermind-lib/build/planner"
	"gophermind/gophermind-lib/build/provider"
)

func TestSlugifyFixtureAnswersTheClarifyStage(t *testing.T) {
	g := newRigFor(t, approving(), slugify, slugifyID)
	req := provider.Request{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "GopherMind planner. Stage: clarify"}}}
	if got := planner.StageOf(req); got != "clarify" {
		t.Fatalf("StageOf(req) = %q, want clarify (the system line prefix changed)", got)
	}
	resp, err := g.fake.Complete(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(slugify, "clarify.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != string(want) {
		t.Fatalf("clarify reply = %q, want the fixture", resp.Text)
	}
}
