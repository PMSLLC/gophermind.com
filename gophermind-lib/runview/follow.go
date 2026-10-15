package runview

import (
	"bytes"
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

	sc       eventScanner
	shrank   bool // the events file shrank below sc.off: node events stopped
	seq      int64
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

// eventScanner splits _state/events.jsonl into lines by the one rule that
// Follow and EventSeq share, so their line numbers cannot drift: a line ends at
// '\n', a partial last line is not a line yet, and a line longer than
// maxEventChunk counts once while the rest of its bytes are skipped unread
// into memory, across as many reads and polls as it takes.
type eventScanner struct {
	off      int64 // bytes consumed
	skipping bool  // inside an over-long line that was already counted
}

// scan calls fn for each whole line after off (ln nil and oversized true for
// an over-long line), reading at most maxEventChunk bytes at a time, until fn
// returns false or no whole line is left. shrank reports that the file is now
// shorter than what was already read.
func (s *eventScanner) scan(path string, fn func(ln []byte, oversized bool) bool) (shrank bool) {
	if !realFile(path) {
		return false
	}
	fh, err := os.Open(path)
	if err != nil {
		return false
	}
	defer fh.Close()
	if fi, err := fh.Stat(); err != nil || fi.Size() < s.off {
		return err == nil
	}
	for {
		data, err := io.ReadAll(io.NewSectionReader(fh, s.off, maxEventChunk))
		if err != nil || len(data) == 0 {
			return false
		}
		base, pos := s.off, 0
		if s.skipping {
			i := bytes.IndexByte(data, '\n')
			if i < 0 {
				s.off += int64(len(data))
				continue
			}
			pos = i + 1
			s.skipping = false
			s.off = base + int64(pos)
		}
		for pos < len(data) {
			i := bytes.IndexByte(data[pos:], '\n')
			if i < 0 {
				if pos == 0 && len(data) == maxEventChunk {
					s.off = base + int64(len(data))
					s.skipping = true
					if !fn(nil, true) {
						return false
					}
				}
				break
			}
			ln := data[pos : pos+i]
			pos += i + 1
			s.off = base + int64(pos)
			if !fn(ln, false) {
				return false
			}
		}
		if s.off == base {
			return false
		}
	}
}

// step reads the new lines, emits their events, then the derived ones. Once
// the events file shrinks below what was read (truncated or replaced), node
// events stop: the line numbers already sent no longer name the same lines.
func (f *follower) step() bool {
	progressed, alive := false, true
	if !f.shrank {
		shrank := f.sc.scan(f.v.eventsPath(), func(ln []byte, oversized bool) bool {
			f.seq++
			progressed = true
			if oversized {
				f.v.warn(fmt.Sprintf("events.jsonl line %d: it is longer than %d bytes", f.seq, maxEventChunk))
				return true
			}
			alive = f.line(ln)
			return alive
		})
		if !alive {
			return false
		}
		if shrank {
			f.shrank = true
			f.v.warn(fmt.Sprintf("events.jsonl line %d: the file shrank below this line; node events stopped", f.seq))
		}
	}
	return f.derived(progressed)
}

func (f *follower) line(ln []byte) bool {
	if len(bytes.TrimSpace(ln)) == 0 {
		return true
	}
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
