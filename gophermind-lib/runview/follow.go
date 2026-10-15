package runview

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/report"
	"gophermind/gophermind-lib/briefv2/tree"
)

// maxEventChunk bounds how many bytes of _state/events.jsonl one poll reads.
const maxEventChunk = 1 << 20

// logLine is one line of _state/events.jsonl, as blackboard.FS writes it.
type logLine struct {
	NodeID string `json:"node_id"`
	Kind   string `json:"kind"`
}

// Follow streams the run's events. Lines of _state/events.jsonl after the
// first `since` become node-status, node-attempt and node-result events (a
// revision line is a node-status carrying the new revision); lines up to
// `since` are read only to keep the per-node attempt counters right. After
// that, run-status, wave-changed and run-report are derived from state as it
// changes: their baseline is taken here, before the goroutine reads any line,
// so a change that lands during the replay is still announced. The channel is
// closed when ctx ends. Follow never writes.
func (v *View) Follow(ctx context.Context, since int64) (<-chan Event, error) {
	if since < 0 {
		since = 0
	}
	ch := make(chan Event)
	f := &follower{v: v, ctx: ctx, ch: ch, since: since, attempts: map[string]int{}}
	f.status, f.reason = v.status()
	f.reported = v.exists(report.FileName)
	f.wave = v.activeWave(ctx)
	go func() {
		defer close(ch)
		f.run()
	}()
	return ch, nil
}

type follower struct {
	v     *View
	ctx   context.Context
	ch    chan<- Event
	since int64

	off, seq int64
	attempts map[string]int // attempt lines seen per node, replayed ones included
	waves    map[string]int

	status, reason string
	wave           int
	reported       bool
}

func (f *follower) run() {
	tick := time.NewTicker(f.v.poll)
	defer tick.Stop()
	for {
		if f.ctx.Err() != nil || !f.step() {
			return
		}
		select {
		case <-f.ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (f *follower) send(e Event) bool {
	select {
	case f.ch <- e:
		return true
	case <-f.ctx.Done():
		return false
	}
}

func (f *follower) emit(typ string, data any) bool {
	b, err := json.Marshal(data)
	if err != nil {
		return true
	}
	return f.send(Event{Seq: f.seq, Type: typ, Data: b})
}

// readChunk returns the complete lines of the events file after byte offset
// off, reading at most maxEventChunk bytes, and the offset after the last one.
func (f *follower) readChunk() ([][]byte, bool) {
	p := f.v.eventsPath()
	if !realFile(p) {
		return nil, false
	}
	fh, err := os.Open(p)
	if err != nil {
		return nil, false
	}
	defer fh.Close()
	if _, err := fh.Seek(f.off, io.SeekStart); err != nil {
		return nil, false
	}
	data, err := io.ReadAll(io.LimitReader(fh, maxEventChunk))
	if err != nil {
		return nil, false
	}
	var lines [][]byte
	pos := 0
	for pos < len(data) {
		i := indexNL(data[pos:])
		if i < 0 {
			if len(data) == maxEventChunk && pos == 0 {
				// One line longer than the chunk: count it, skip its head.
				lines = append(lines, nil)
				pos = len(data)
			}
			break
		}
		lines = append(lines, data[pos:pos+i])
		pos += i + 1
	}
	f.off += int64(pos)
	return lines, true
}

func indexNL(b []byte) int {
	for i, c := range b {
		if c == '\n' {
			return i
		}
	}
	return -1
}

// step reads the new lines, emits their events, then the derived ones.
func (f *follower) step() bool {
	progressed := false
	for {
		lines, ok := f.readChunk()
		if !ok || len(lines) == 0 {
			break
		}
		for _, ln := range lines {
			f.seq++
			progressed = true
			if !f.line(ln) {
				return false
			}
		}
	}
	return f.derived(progressed)
}

func (f *follower) line(ln []byte) bool {
	var l logLine
	if json.Unmarshal(ln, &l) != nil || !ValidNodeID(l.NodeID) {
		f.v.warn(fmt.Sprintf("events.jsonl line %d: it is not an event", f.seq))
		return true
	}
	if l.Kind == "attempt" {
		f.attempts[l.NodeID]++
	}
	if f.seq <= f.since {
		return true
	}
	switch blackboard.EventKind(l.Kind) {
	case blackboard.EventStatus, blackboard.EventRevision, blackboard.EventAttempt, blackboard.EventResult:
	default:
		f.v.warn(fmt.Sprintf("events.jsonl line %d: unknown event kind", f.seq))
		return true
	}
	row, err := f.v.bb.Get(f.ctx, f.v.runID, l.NodeID)
	if err != nil {
		f.v.warn(fmt.Sprintf("events.jsonl line %d: the node has no readable runtime file", f.seq))
		return true
	}
	switch blackboard.EventKind(l.Kind) {
	case blackboard.EventStatus, blackboard.EventRevision:
		return f.emit(EventNodeStatus, NodeStatusData{ID: l.NodeID, Status: string(row.Status), Wave: f.waveOf(l.NodeID), Revision: row.Revision})
	case blackboard.EventAttempt:
		if len(row.Attempts) == 0 {
			return true
		}
		i := f.attempts[l.NodeID] - 1
		if i >= len(row.Attempts) {
			i = len(row.Attempts) - 1
		}
		return f.emit(EventNodeAttempt, NodeAttemptData{ID: l.NodeID, Attempt: wireAttempt(row.Attempts[i])})
	default: // result
		if row.Result == nil {
			return true
		}
		return f.emit(EventNodeResult, NodeResultData{ID: l.NodeID, Result: wireResult(row.Result)})
	}
}

func (f *follower) waveOf(id string) int {
	if w, ok := f.waves[id]; ok {
		return w
	}
	f.waves = f.v.waveMap()
	return f.waves[id]
}

func (v *View) waveMap() map[string]int {
	m := map[string]int{}
	t, err := tree.NewStore(v.dir).Load()
	if err != nil {
		return m
	}
	for id, n := range t.Nodes {
		if n.Wave != nil {
			m[id] = *n.Wave
		}
	}
	return m
}

// derived emits run-status, wave-changed and run-report when state moved since
// the baseline or the last call. The active wave is recomputed only when new
// lines arrived: reading every runtime file on every tick would cost more than
// it tells.
func (f *follower) derived(progressed bool) bool {
	status, reason := f.v.status()
	reported := f.v.exists(report.FileName)
	wave := f.wave
	if progressed {
		wave = f.v.activeWave(f.ctx)
	}
	if status != f.status || reason != f.reason {
		f.status, f.reason = status, reason
		if !f.emit(EventRunStatus, RunStatusData{Status: status, StopReason: reason}) {
			return false
		}
	}
	if wave != f.wave {
		for _, w := range waveChanges(f.wave, wave) {
			if !f.emit(EventWaveChanged, w) {
				return false
			}
		}
		f.wave = wave
	}
	if reported && !f.reported {
		if body, err := f.v.Report(); err == nil {
			f.reported = true
			return f.send(Event{Seq: f.seq, Type: EventRunReport, Data: json.RawMessage(body)})
		}
	}
	return true
}

// waveChanges is the events for the active wave moving from prev to cur
// (-1 means no wave is active).
func waveChanges(prev, cur int) []WaveChangedData {
	var out []WaveChangedData
	if prev >= 0 {
		out = append(out, WaveChangedData{Wave: prev, State: "finished"})
	}
	if cur >= 0 {
		out = append(out, WaveChangedData{Wave: cur, State: "started"})
	}
	return out
}

// activeWave is the lowest wave that still has a function node to finish, or
// -1. Only verified and failed are finished: needs_revision, escalated,
// claimed, in_progress, ready and pending keep the wave active, because the
// revise loop sends a node from needs_revision back to ready.
func (v *View) activeWave(ctx context.Context) int {
	l := v.load(ctx)
	active := -1
	for id, n := range l.t.Nodes {
		if n.Kind != tree.KindFunction || n.Wave == nil {
			continue
		}
		st := blackboard.StatusPending
		if r, ok := l.rows[id]; ok {
			st = r.Status
		}
		if st == blackboard.StatusVerified || st == blackboard.StatusFailed {
			continue
		}
		if active == -1 || *n.Wave < active {
			active = *n.Wave
		}
	}
	return active
}
