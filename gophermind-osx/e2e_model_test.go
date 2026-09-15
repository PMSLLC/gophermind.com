//go:build e2e

package main

import (
	"context"
	"testing"
	"time"

	"gophermind/gophermind-osx/client"
	"gophermind/gophermind-osx/connection"
	appui "gophermind/gophermind-osx/ui"
	"gophermind/gophermind-lib/modelcat"
)

// TestE2E_LocalMode_ModelSwitching covers "Model switching E2E: pin →
// cycle → switch passes" (.planning/tasks/05-01.json). It exercises the
// full model-switching path against a real gophermind-server:
//
//  1. Pin: create a session with a specific model, verify via SessionConfig.
//  2. Cycle: use ModelPickerState to cycle through the preference order
//     (add, move up/down, remove).
//  3. Switch: patch model settings via the client and verify the change
//     round-trips.
func TestE2E_LocalMode_ModelSwitching(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E in -short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Start a real server.
	cm := connection.New(connection.BackendConfig{
		Name: "e2e-model",
		Mode: connection.ModeLocal,
		Local: connection.LocalConfig{
			ServerBinaryPath: e2eBuildServerBinary(t),
		},
	})
	if err := cm.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer cm.Disconnect()

	cl := cm.Client()

	// --- Pin ---
	// Create a session pinned to a specific model.
	sessionID, err := cl.CreateSession(ctx, client.CreateSessionOptions{
		Model: "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer cl.DeleteSession(ctx, sessionID)

	// Verify the session config reports the pinned model.
	cfg, err := cl.SessionConfig(ctx, sessionID)
	if err != nil {
		t.Fatalf("SessionConfig: %v", err)
	}
	if cfg.Model != "test-model" {
		t.Fatalf("pinned model = %q, want %q", cfg.Model, "test-model")
	}
	t.Log("Pin verified")

	// --- Cycle ---
	// Build a ModelPickerState with a small catalogue and cycle through
	// the preference order.
	entries := []modelcat.Entry{
		{ID: "model-a", Profile: "p1", Provider: "prov1", Modality: "text", Reachable: true},
		{ID: "model-b", Profile: "p1", Provider: "prov1", Modality: "text", Reachable: true},
		{ID: "model-c", Profile: "p2", Provider: "prov2", Modality: "text", Reachable: true},
	}
	picker := appui.NewModelPickerState(entries, modelcat.Settings{})

	// Add models to the preference order.
	picker.AddToOrder("p1/model-a")
	picker.AddToOrder("p1/model-b")
	picker.AddToOrder("p2/model-c")

	order := picker.Order()
	if len(order) != 3 {
		t.Fatalf("order length = %d, want 3", len(order))
	}
	if order[0] != "p1/model-a" || order[1] != "p1/model-b" || order[2] != "p2/model-c" {
		t.Fatalf("order = %v, want [p1/model-a p1/model-b p2/model-c]", order)
	}

	// Cycle: move model-c to the front (two positions up).
	picker.MoveUp("p2/model-c")
	picker.MoveUp("p2/model-c")
	order = picker.Order()
	if order[0] != "p2/model-c" {
		t.Fatalf("after MoveUp x2, order[0] = %q, want p2/model-c", order[0])
	}

	// Cycle: move model-a to the back.
	picker.MoveDown("p1/model-a")
	picker.MoveDown("p1/model-a")
	order = picker.Order()
	if order[2] != "p1/model-a" {
		t.Fatalf("after MoveDown x2, order[2] = %q, want p1/model-a", order[2])
	}

	// Remove a model from the order.
	picker.RemoveFromOrder("p1/model-b")
	order = picker.Order()
	if len(order) != 2 {
		t.Fatalf("after RemoveFromOrder, order length = %d, want 2", len(order))
	}
	for _, k := range order {
		if k == "p1/model-b" {
			t.Fatalf("p1/model-b still in order after removal: %v", order)
		}
	}

	// Set the current key (simulates pinning a model in the UI).
	picker.SetCurrentKey("p2/model-c")
	if !picker.IsCurrent(entries[2]) {
		t.Fatal("IsCurrent(model-c) = false, want true")
	}
	if picker.IsCurrent(entries[0]) {
		t.Fatal("IsCurrent(model-a) = true, want false")
	}
	t.Log("Cycle verified")

	// --- Switch ---
	// Patch model settings and verify the change round-trips.
	err = cl.PatchModelSettings(ctx, map[string]any{
		"order": []string{"p2/model-c", "p1/model-a"},
	})
	if err != nil {
		t.Fatalf("PatchModelSettings: %v", err)
	}

	settings, err := cl.ModelSettings(ctx)
	if err != nil {
		t.Fatalf("ModelSettings: %v", err)
	}
	if len(settings.Order) != 2 {
		t.Fatalf("settings.Order length = %d, want 2", len(settings.Order))
	}
	if settings.Order[0] != "p2/model-c" || settings.Order[1] != "p1/model-a" {
		t.Fatalf("settings.Order = %v, want [p2/model-c p1/model-a]", settings.Order)
	}
	t.Log("Switch verified")

	t.Log("Model switching E2E: passed")
}
