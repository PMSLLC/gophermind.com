//go:build e2e

package main

// This file is 05-06's performance baseline (.planning/tasks/05-06.json):
// message round-trip latency (local vs remote), SSE streaming throughput,
// and the WireGuard tunnel's latency overhead, all measured against real
// gophermind-server instances via the same e2eConnectLocal/e2eConnectRemote
// helpers e2e_local_test.go and e2e_remote_test.go use -- no synthetic
// timing, no mocks. Those helpers take testing.TB (rather than *testing.T)
// specifically so this file's Benchmarks can call them directly.
//
// BenchmarkStreamingThroughput_Local's tokens/sec number is dominated by
// the configured LLM's real generation speed, not by this app's transport
// overhead -- that is the honest, correct thing to measure for "SSE
// streaming throughput," but it also means the number is only comparable
// across runs against the same model/endpoint, not a fixed absolute
// target. Run it with `-benchtime=1x` to avoid go test's default
// time-based iteration count hammering a real LLM endpoint far more than
// needed to get one real reading; the healthz latency benchmarks are cheap
// enough that the default iteration count is fine.

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"gophermind/gophermind-osx/client"
)

// BenchmarkHealthzLatency_Local measures round-trip latency for the
// cheapest possible real request (GET /healthz) against a locally spawned
// gophermind-server -- the "message round-trip latency, local" baseline,
// isolated from any LLM inference time.
func BenchmarkHealthzLatency_Local(b *testing.B) {
	conn := e2eConnectLocal(b)
	cl := conn.Client()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !cl.Healthy(ctx) {
			b.Fatal("Healthy() = false mid-benchmark")
		}
	}
}

// BenchmarkHealthzLatency_Remote is BenchmarkHealthzLatency_Local's remote
// counterpart, through a real WireGuard tunnel (e2eConnectRemote, same
// in-process serve.NewMux backend e2e_remote_test.go uses). Comparing its
// ns/op against BenchmarkHealthzLatency_Local's is 05-06's "WG overhead:
// local vs remote latency delta measured."
func BenchmarkHealthzLatency_Remote(b *testing.B) {
	conn := e2eConnectRemote(b)
	cl := conn.Client()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !cl.Healthy(ctx) {
			b.Fatal("Healthy() = false mid-benchmark")
		}
	}
}

// BenchmarkStreamingThroughput_Local measures real SSE token throughput: a
// chat turn that asks for a moderately long response, timing from the
// first byte to "done", reported as a custom tokens/sec metric (run with
// -benchtime=1x; see this file's top doc comment for why more than one
// real LLM turn per run is wasteful).
func BenchmarkStreamingThroughput_Local(b *testing.B) {
	conn := e2eConnectLocal(b)
	cl := conn.Client()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	sessionID, err := cl.CreateSession(ctx, client.CreateSessionOptions{})
	if err != nil {
		b.Fatalf("CreateSession: %v", err)
	}

	b.ResetTimer()
	var totalTokens int
	var totalElapsed time.Duration
	for i := 0; i < b.N; i++ {
		stream, err := cl.Stream(ctx, sessionID, "Count from one to fifty, one number per line.")
		if err != nil {
			b.Fatalf("Stream: %v", err)
		}
		start := time.Now()
		tokens := 0
		for {
			ev, err := stream.Next()
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				b.Fatalf("stream.Next: %v", err)
			}
			if ev.Type == "token" {
				tokens++
			}
			if ev.Type == "done" {
				break
			}
		}
		stream.Close()
		totalTokens += tokens
		totalElapsed += time.Since(start)
	}
	b.StopTimer()

	if totalElapsed > 0 {
		b.ReportMetric(float64(totalTokens)/totalElapsed.Seconds(), "tokens/sec")
	}
}
