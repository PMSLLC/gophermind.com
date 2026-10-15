package runview_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/report"
	"gophermind/gophermind-lib/briefv2/runfs"
	"gophermind/gophermind-lib/runview"
	"gophermind/gophermind-lib/runview/runviewtest"
)

func follow(t *testing.T, st runviewtest.Stage, since func(*runview.View) int64) (*runview.View, runviewtest.Fixture, <-chan runview.Event, context.CancelFunc) {
	t.Helper()
	root := t.TempDir()
	f := runviewtest.Build(t, root, st)
	v, err := runview.Open(root, runviewtest.RunID, runview.WithPoll(10*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ch, err := v.Follow(ctx, since(v))
	if err != nil {
		t.Fatal(err)
	}
	return v, f, ch, cancel
}

func next(t *testing.T, ch <-chan runview.Event) runview.Event {
	t.Helper()
	select {
	case e, ok := <-ch:
		if !ok {
			t.Fatal("stream closed")
		}
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for an event")
	}
	return runview.Event{}
}

// until reads events until pred accepts one, returning it and everything before.
func until(t *testing.T, ch <-chan runview.Event, pred func(runview.Event) bool) (runview.Event, []runview.Event) {
	t.Helper()
	var seen []runview.Event
	for {
		e := next(t, ch)
		if pred(e) {
			return e, seen
		}
		seen = append(seen, e)
	}
}

func ofType(typ string) func(runview.Event) bool {
	return func(e runview.Event) bool { return e.Type == typ }
}

func TestFollowFromZeroReplaysEveryLineOnce(t *testing.T) {
	v, _, ch, _ := follow(t, runviewtest.Building, func(*runview.View) int64 { return 0 })
	n := v.EventSeq()
	if n < 8 {
		t.Fatalf("fixture log has only %d lines", n)
	}
	var attempts int
	for i := int64(1); i <= n; i++ {
		e := next(t, ch)
		if e.Seq != i {
			t.Fatalf("event %d has seq %d", i, e.Seq)
		}
		if !strings.HasPrefix(e.Type, "node-") {
			t.Fatalf("replay produced %q; only node events are replayed", e.Type)
		}
		if e.Type == runview.EventNodeAttempt {
			var d runview.NodeAttemptData
			if err := json.Unmarshal(e.Data, &d); err != nil || d.Attempt.Model == "" {
				t.Fatalf("attempt data = %s, %v", e.Data, err)
			}
			attempts++
		}
	}
	if attempts != 3 { // fn-greet fail and pass, fn-bye fail
		t.Errorf("%d attempt events, want 3", attempts)
	}
	select {
	case e := <-ch:
		t.Fatalf("an extra event after the replay: %+v", e)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestFollowReplayShowsEachAttemptWithItsOwnData(t *testing.T) {
	_, _, ch, _ := follow(t, runviewtest.Building, func(*runview.View) int64 { return 0 })
	var verdicts []string
	for len(verdicts) < 3 {
		e := next(t, ch)
		if e.Type != runview.EventNodeAttempt {
			continue
		}
		var d runview.NodeAttemptData
		_ = json.Unmarshal(e.Data, &d)
		verdicts = append(verdicts, d.ID+":"+d.Attempt.Verdict)
	}
	if got := strings.Join(verdicts, ","); got != "fn-greet:fail,fn-greet:pass,fn-bye:fail" {
		t.Errorf("attempt events = %s (the n-th attempt line must map to the n-th attempt)", got)
	}
}

func TestFollowSinceSkipsEarlierLines(t *testing.T) {
	var n int64
	_, _, ch, _ := follow(t, runviewtest.Building, func(v *runview.View) int64 { n = v.EventSeq(); return n - 2 })
	e1, e2 := next(t, ch), next(t, ch)
	if e1.Seq != n-1 || e2.Seq != n {
		t.Errorf("seqs = %d, %d; want %d, %d", e1.Seq, e2.Seq, n-1, n)
	}
}

func TestFollowLiveRunToTheEnd(t *testing.T) {
	_, f, ch, _ := follow(t, runviewtest.Building, func(v *runview.View) int64 { return v.EventSeq() })
	ctx := context.Background()

	// fn-bye passes its tests.
	if err := f.BB.SetStatus(ctx, f.RunID, "fn-bye", "in_progress", "verified"); err != nil {
		t.Fatal(err)
	}
	e := next(t, ch)
	var st runview.NodeStatusData
	_ = json.Unmarshal(e.Data, &st)
	if e.Type != runview.EventNodeStatus || st.ID != "fn-bye" || st.Status != "verified" || st.Wave != 1 {
		t.Fatalf("event = %s %s", e.Type, e.Data)
	}

	// The last node finishes: wave 1 is over.
	runviewtest.Verify(t, f, "fn-late")
	w, _ := until(t, ch, ofType(runview.EventWaveChanged))
	var wc runview.WaveChangedData
	_ = json.Unmarshal(w.Data, &wc)
	if wc.Wave != 1 || wc.State != "finished" {
		t.Errorf("wave event = %+v, want wave 1 finished", wc)
	}

	// The executor writes its report: run-status, then run-report.
	runviewtest.WriteReport(t, f, "verified", "", 0, report.NodeCounts{Total: 3, Verified: 3})
	rs, _ := until(t, ch, ofType(runview.EventRunStatus))
	var rd runview.RunStatusData
	_ = json.Unmarshal(rs.Data, &rd)
	if rd.Status != "verified" {
		t.Errorf("run-status = %+v", rd)
	}
	rr, _ := until(t, ch, ofType(runview.EventRunReport))
	var body runview.ReportBody
	if err := json.Unmarshal(rr.Data, &body); err != nil || !body.IsComplete() || body.Status != "verified" {
		t.Errorf("run-report = %s, %v", rr.Data, err)
	}
	select {
	case e := <-ch:
		t.Errorf("run-report must come once, got %s after it", e.Type)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestFollowSkipsACorruptLineAndReportsIt(t *testing.T) {
	v, f, ch, _ := follow(t, runviewtest.Building, func(v *runview.View) int64 { return v.EventSeq() })
	bad := v.EventSeq() + 1
	if err := runfs.AppendLine(filepath.Join(f.Dir, "_state", "events.jsonl"), []byte("this is not json")); err != nil {
		t.Fatal(err)
	}
	if err := f.BB.SetStatus(context.Background(), f.RunID, "fn-bye", "in_progress", "verified"); err != nil {
		t.Fatal(err)
	}
	e := next(t, ch)
	if e.Type != runview.EventNodeStatus || e.Seq != bad+1 {
		t.Fatalf("event after the corrupt line = %s seq %d, want node-status seq %d (line numbers stay true)", e.Type, e.Seq, bad+1)
	}
	joined := strings.Join(v.Warnings(), "|")
	if !strings.Contains(joined, "line "+itoa(bad)) || strings.Contains(joined, "this is not json") {
		t.Errorf("warnings = %q: want the line number, never the content", joined)
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestFollowStopsWhenTheContextEnds(t *testing.T) {
	_, _, ch, cancel := follow(t, runviewtest.Planned, func(*runview.View) int64 { return 0 })
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("an event after cancel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the channel did not close after the context was cancelled")
	}
}

func TestFollowNeverWrites(t *testing.T) {
	root := t.TempDir()
	f := runviewtest.Build(t, root, runviewtest.Building)
	before := runviewtest.Digest(t, root)
	v, _ := runview.Open(root, runviewtest.RunID, runview.WithPoll(10*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	ch, _ := v.Follow(ctx, 0)
	for i := int64(0); i < v.EventSeq(); i++ {
		next(t, ch)
	}
	cancel()
	time.Sleep(50 * time.Millisecond)
	if runviewtest.Digest(t, root) != before {
		t.Fatal("Follow changed the run folder")
	}
	_ = f
}
func noEvent(t *testing.T, ch <-chan runview.Event) {
	t.Helper()
	select {
	case e := <-ch:
		t.Fatalf("unexpected event %s %s", e.Type, e.Data)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestFollowRevisionLineEmitsNodeStatusWithRevision(t *testing.T) {
	v, f, ch, _ := follow(t, runviewtest.Building, func(v *runview.View) int64 { return v.EventSeq() })
	if err := f.BB.SetRevision(context.Background(), f.RunID, "fn-late", 1); err != nil {
		t.Fatal(err)
	}
	e := next(t, ch)
	var st runview.NodeStatusData
	_ = json.Unmarshal(e.Data, &st)
	if e.Type != runview.EventNodeStatus || st.ID != "fn-late" || st.Revision != 1 || st.Status != "pending" || st.Wave != 1 {
		t.Fatalf("event = %s %s", e.Type, e.Data)
	}
	noEvent(t, ch)
	if w := v.Warnings(); len(w) != 0 {
		t.Errorf("warnings = %v, want none", w)
	}
}

func TestFollowStatusLineCarriesTheCurrentRevision(t *testing.T) {
	_, f, ch, _ := follow(t, runviewtest.Building, func(v *runview.View) int64 { return v.EventSeq() })
	ctx := context.Background()
	if err := f.BB.SetRevision(ctx, f.RunID, "fn-bye", 2); err != nil {
		t.Fatal(err)
	}
	next(t, ch)
	if err := f.BB.SetStatus(ctx, f.RunID, "fn-bye", "in_progress", "verified"); err != nil {
		t.Fatal(err)
	}
	var st runview.NodeStatusData
	_ = json.Unmarshal(next(t, ch).Data, &st)
	if st.Status != "verified" || st.Revision != 2 {
		t.Errorf("status event = %+v", st)
	}
}

func TestFollowRevisionLoopDoesNotFlapTheWave(t *testing.T) {
	_, f, ch, _ := follow(t, runviewtest.Building, func(v *runview.View) int64 { return v.EventSeq() })
	ctx := context.Background()
	runviewtest.Verify(t, f, "fn-late")
	// fn-bye is the last open leaf of wave 1: in_progress -> needs_revision -> ready.
	for _, tr := range [][2]string{{"in_progress", "needs_revision"}, {"needs_revision", "ready"}} {
		if err := f.BB.SetStatus(ctx, f.RunID, "fn-bye", blackboard.Status(tr[0]), blackboard.Status(tr[1])); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.After(300 * time.Millisecond)
	n := 0
	for {
		select {
		case e := <-ch:
			if e.Type == runview.EventWaveChanged {
				t.Fatalf("wave-changed during the revise loop: %s", e.Data)
			}
			n++
		case <-deadline:
			if n < 3 {
				t.Fatalf("saw only %d events", n)
			}
			return
		}
	}
}

func TestFollowBaselineIsTakenBeforeTheReplay(t *testing.T) {
	root := t.TempDir()
	f := runviewtest.Build(t, root, runviewtest.Building)
	v, _ := runview.Open(root, runviewtest.RunID, runview.WithPoll(10*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, _ := v.Follow(ctx, 0)
	// The run ends right after Follow returns, before the first line is read.
	if err := f.BB.SetStatus(context.Background(), f.RunID, "fn-bye", "in_progress", "verified"); err != nil {
		t.Fatal(err)
	}
	runviewtest.Verify(t, f, "fn-late")
	runviewtest.WriteReport(t, f, "verified", "", 0, report.NodeCounts{Total: 3, Verified: 3})
	until(t, ch, ofType(runview.EventRunStatus))
	until(t, ch, ofType(runview.EventRunReport))
}

func TestFollowCapsWarningsAtTwenty(t *testing.T) {
	v, f, ch, _ := follow(t, runviewtest.Building, func(v *runview.View) int64 { return v.EventSeq() })
	for i := 0; i < 40; i++ {
		if err := runfs.AppendLine(filepath.Join(f.Dir, "_state", "events.jsonl"), []byte("junk")); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.BB.SetStatus(context.Background(), f.RunID, "fn-bye", "in_progress", "verified"); err != nil {
		t.Fatal(err)
	}
	next(t, ch)
	if n := len(v.Warnings()); n != 20 {
		t.Errorf("%d warnings, want the cap of 20", n)
	}
}

func TestFollowLeaksNoGoroutines(t *testing.T) {
	root := t.TempDir()
	runviewtest.Build(t, root, runviewtest.Building)
	v, _ := runview.Open(root, runviewtest.RunID, runview.WithPoll(5*time.Millisecond))
	base := runtime.NumGoroutine()
	var chans []<-chan runview.Event
	var cancels []context.CancelFunc
	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		ch, _ := v.Follow(ctx, 0) // nobody reads: each goroutine blocks in send
		chans = append(chans, ch)
		cancels = append(cancels, cancel)
	}
	time.Sleep(50 * time.Millisecond)
	for _, c := range cancels {
		c()
	}
	for _, ch := range chans {
		deadline := time.After(2 * time.Second)
	drain:
		for {
			select {
			case _, ok := <-ch:
				if !ok {
					break drain
				}
			case <-deadline:
				t.Fatal("a stream did not close after cancel")
			}
		}
	}
	for i := 0; i < 100 && runtime.NumGoroutine() > base; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > base {
		t.Errorf("goroutines %d after cancel, %d before", n, base)
	}
}

// appendRaw appends parts to the run's events file, pausing between them so
// the follower polls in between and sees a line arrive in pieces.
func appendRaw(t *testing.T, f runviewtest.Fixture, parts ...[]byte) {
	t.Helper()
	for i, p := range parts {
		if i > 0 {
			time.Sleep(40 * time.Millisecond)
		}
		fh, err := os.OpenFile(filepath.Join(f.Dir, "_state", "events.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fh.Write(p); err != nil {
			t.Fatal(err)
		}
		if err := fh.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFollowCountsAnOversizedLineOnce(t *testing.T) {
	big := runview.MaxEventChunkForTest
	cases := []struct {
		name  string
		parts [][]byte
	}{
		{"chunk plus ten", [][]byte{[]byte(strings.Repeat("x", big+10) + "\n")}},
		{"two and a half chunks in pieces", [][]byte{
			[]byte(strings.Repeat("x", big*3/2)),
			[]byte(strings.Repeat("x", big) + "\n"),
		}},
		{"exactly a chunk, newline later", [][]byte{
			[]byte(strings.Repeat("x", big)),
			[]byte("\n"),
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, f, ch, _ := follow(t, runviewtest.Building, func(v *runview.View) int64 { return v.EventSeq() })
			long := v.EventSeq() + 1
			appendRaw(t, f, tc.parts...)
			time.Sleep(40 * time.Millisecond)
			if err := f.BB.SetStatus(context.Background(), f.RunID, "fn-bye", "in_progress", "verified"); err != nil {
				t.Fatal(err)
			}
			e := next(t, ch)
			if e.Type != runview.EventNodeStatus || e.Seq != long+1 {
				t.Fatalf("event = %s seq %d, want node-status seq %d", e.Type, e.Seq, long+1)
			}
			if n := v.EventSeq(); n != e.Seq {
				t.Errorf("EventSeq = %d, the last event's seq = %d", n, e.Seq)
			}
			noEvent(t, ch)
			w := v.Warnings()
			if len(w) != 1 || !strings.Contains(w[0], "line "+itoa(long)) {
				t.Errorf("warnings = %q, want one for line %d", w, long)
			}
			// A fresh replay numbers the lines the same way.
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			rch, _ := v.Follow(ctx, 0)
			var last runview.Event
			for i := int64(0); i < e.Seq-1; i++ { // the long line emits nothing
				last = next(t, rch)
			}
			if last.Seq != e.Seq {
				t.Errorf("replay ends at seq %d, want %d", last.Seq, e.Seq)
			}
		})
	}
}

func TestFollowSkipsBlankLinesSilently(t *testing.T) {
	v, f, ch, _ := follow(t, runviewtest.Building, func(v *runview.View) int64 { return v.EventSeq() })
	first := v.EventSeq() + 1
	appendRaw(t, f, []byte("\n   \t\n"))
	if err := f.BB.SetStatus(context.Background(), f.RunID, "fn-bye", "in_progress", "verified"); err != nil {
		t.Fatal(err)
	}
	e := next(t, ch)
	if e.Type != runview.EventNodeStatus || e.Seq != first+2 {
		t.Fatalf("event = %s seq %d, want node-status seq %d (blank lines still count)", e.Type, e.Seq, first+2)
	}
	if w := v.Warnings(); len(w) != 0 {
		t.Errorf("warnings = %q, want none for blank lines", w)
	}
}

func TestFollowStopsNodeEventsWhenTheLogShrinks(t *testing.T) {
	v, f, ch, _ := follow(t, runviewtest.Building, func(v *runview.View) int64 { return v.EventSeq() })
	ctx := context.Background()
	// Wait for one live event, so the follower has read the whole log.
	if err := f.BB.SetStatus(ctx, f.RunID, "fn-bye", "in_progress", "verified"); err != nil {
		t.Fatal(err)
	}
	n := next(t, ch).Seq
	path := filepath.Join(f.Dir, "_state", "events.jsonl")
	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}
	if err := f.BB.SetRevision(ctx, f.RunID, "fn-late", 1); err != nil {
		t.Fatal(err)
	}
	noEvent(t, ch)
	w := v.Warnings()
	if len(w) != 1 || !strings.Contains(w[0], "line "+itoa(n)) || strings.Contains(w[0], f.Dir) {
		t.Errorf("warnings = %q, want one naming line %d and no path", w, n)
	}
}
