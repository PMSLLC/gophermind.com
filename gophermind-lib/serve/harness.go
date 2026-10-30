package serve

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// The harness runs the example briefs again and again, unattended, until each
// one has passed once. It submits one brief at a time through the brief
// manager, waits for the run to end, records the result and moves on to the
// next brief that has not passed yet. Its state is a file, so it carries on
// after a restart or a reboot; a run the restart cut off counts as a failed
// attempt. It never changes code: it only repeats runs and keeps the score.

// harnessEvery is how often the harness looks at its run. A variable so tests
// can shorten it.
var harnessEvery = 20 * time.Second

// harnessStateFile is the harness's state, kept in the briefs folder. Its name
// starts with a dot so it is never read as a run.
const harnessStateFile = ".harness.json"

type harnessBrief struct {
	File      string `json:"file"`
	Title     string `json:"title"`
	Bytes     int    `json:"bytes"`
	Attempts  int    `json:"attempts"`
	Passes    int    `json:"passes"`
	LastRun   string `json:"last_run,omitempty"`
	LastPhase string `json:"last_phase,omitempty"`
	LastAt    string `json:"last_at,omitempty"`
}

type harnessState struct {
	// Running is whether the harness starts runs. It stays on across restarts.
	Running bool `json:"running"`
	// Done is set when every brief has passed; the harness then stops.
	Done bool `json:"done"`
	// Current is the run the harness started and is waiting for.
	Current     string         `json:"current,omitempty"`
	CurrentFile string         `json:"current_file,omitempty"`
	Next        int            `json:"next"`
	Briefs      []harnessBrief `json:"briefs"`
	Note        string         `json:"note,omitempty"`
	UpdatedAt   string         `json:"updated_at,omitempty"`
}

type harness struct {
	m     *briefManager
	dir   string // the example briefs
	path  string // the state file
	log   *slog.Logger
	every time.Duration

	mu sync.Mutex
	st harnessState
}

func newHarness(m *briefManager, dir string) *harness {
	h := &harness{m: m, dir: dir, path: filepath.Join(m.dir, harnessStateFile), log: m.log, every: harnessEvery}
	if b, err := os.ReadFile(h.path); err == nil {
		_ = json.Unmarshal(b, &h.st)
	}
	h.mu.Lock()
	h.syncBriefs()
	h.mu.Unlock()
	return h
}

// syncBriefs brings the list in line with the folder: every .md file but the
// README, smallest first, keeping the score of the files it already knew.
func (h *harness) syncBriefs() {
	ents, err := os.ReadDir(h.dir)
	if err != nil {
		h.st.Note = "cannot read the example briefs: " + err.Error()
		return
	}
	known := map[string]harnessBrief{}
	for _, b := range h.st.Briefs {
		known[b.File] = b
	}
	var list []harnessBrief
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") || strings.EqualFold(name, "README.md") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(h.dir, name))
		if err != nil {
			continue
		}
		b := known[name]
		b.File, b.Bytes, b.Title = name, len(src), briefTitle(src)
		list = append(list, b)
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Bytes != list[j].Bytes {
			return list[i].Bytes < list[j].Bytes
		}
		return list[i].File < list[j].File
	})
	h.st.Briefs = list
}

// briefTitle is the frontmatter title of a brief, "" when it has none.
func briefTitle(src []byte) string {
	for _, line := range strings.Split(string(src), "\n") {
		if t, ok := strings.CutPrefix(strings.TrimSpace(line), "title:"); ok {
			return strings.Trim(strings.TrimSpace(t), `"'`)
		}
	}
	return ""
}

func (h *harness) save() {
	h.st.UpdatedAt = h.m.stamp()
	b, err := json.MarshalIndent(h.st, "", "  ")
	if err != nil {
		return
	}
	tmp := h.path + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, h.path)
	}
}

// loop ticks until the process ends.
func (h *harness) loop() {
	for {
		h.tick()
		time.Sleep(h.every)
	}
}

// tick records the harness's run once it has ended, then starts the next
// brief that has not passed, when the slot is free.
func (h *harness) tick() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.st.Current != "" {
		rec, ok := h.m.readRecord(h.st.Current)
		if ok && (rec.Phase == phasePlanning || rec.Phase == phaseRunning) {
			return
		}
		phase := rec.Phase
		if !ok {
			phase = "lost"
		}
		h.record(h.st.CurrentFile, phase)
		h.log.Info("harness run ended", "run_id", h.st.Current, "brief", h.st.CurrentFile, "phase", phase)
		h.st.Current, h.st.CurrentFile = "", ""
		h.save()
	}
	// A parked run that was answered and resumed becomes the harness's run
	// again; one that ended while parked is counted.
	waiting := 0
	for i := range h.st.Briefs {
		b := &h.st.Briefs[i]
		if b.LastPhase != phaseWaiting {
			continue
		}
		rec, ok := h.m.readRecord(b.LastRun)
		switch {
		case ok && rec.Phase == phaseWaiting:
			waiting++
		case ok && (rec.Phase == phasePlanning || rec.Phase == phaseRunning):
			h.st.Current, h.st.CurrentFile, h.st.Note = b.LastRun, b.File, ""
			h.log.Info("harness resumed an answered run", "run_id", b.LastRun, "brief", b.File)
			h.save()
			return
		default:
			phase := rec.Phase
			if !ok {
				phase = "lost"
			}
			h.record(b.File, phase)
			h.save()
		}
	}
	if !h.st.Running {
		return
	}
	h.syncBriefs()
	if len(h.st.Briefs) == 0 {
		h.st.Note = "no example briefs found in " + h.dir
		h.save()
		return
	}
	idx := -1
	for k := 0; k < len(h.st.Briefs); k++ {
		i := (h.st.Next + k) % len(h.st.Briefs)
		if h.st.Briefs[i].Passes == 0 && h.st.Briefs[i].LastPhase != phaseWaiting {
			idx = i
			break
		}
	}
	if idx < 0 && waiting > 0 {
		h.st.Note = waitingNote(waiting)
		return
	}
	if idx < 0 {
		h.st.Running, h.st.Done, h.st.Note = false, true, "every example brief has passed"
		h.log.Info("harness done: every example brief has passed")
		h.save()
		return
	}
	if active := h.m.activeID(); active != "" {
		h.st.Note = "waiting: another run is active (" + active + ")"
		return
	}
	// An answered run goes before a new one; the next tick picks it up.
	if h.m.resumeAnswered() != "" {
		return
	}
	b := &h.st.Briefs[idx]
	src, err := os.ReadFile(filepath.Join(h.dir, b.File))
	if err != nil {
		h.st.Note = "cannot read " + b.File
		h.save()
		return
	}
	parsed, src, err := h.m.withFreshID(src)
	if err != nil {
		h.st.Note = b.File + " does not parse: " + err.Error()
		h.st.Next = idx + 1
		h.save()
		return
	}
	// Planning asks its questions on the run's page rather than failing on
	// them; a run that stops to ask is parked until it is answered.
	rec, err := h.m.start(parsed, src, true)
	var busy *briefBusyError
	if errors.As(err, &busy) {
		h.st.Note = "waiting: another run is active (" + busy.activeID + ")"
		return
	}
	if err != nil {
		h.st.Note = "cannot start " + b.File + ": " + err.Error()
		h.save()
		return
	}
	b.Attempts++
	b.LastRun = rec.ID
	h.st.Current, h.st.CurrentFile, h.st.Next, h.st.Note = rec.ID, b.File, idx+1, ""
	h.log.Info("harness started a run", "run_id", rec.ID, "brief", b.File, "attempt", b.Attempts)
	h.save()
}

// record notes how a brief's latest run ended.
func (h *harness) record(file, phase string) {
	for i := range h.st.Briefs {
		if h.st.Briefs[i].File == file {
			h.st.Briefs[i].LastPhase, h.st.Briefs[i].LastAt = phase, h.m.stamp()
			if phase == phaseDone {
				h.st.Briefs[i].Passes++
			}
		}
	}
}

func waitingNote(n int) string {
	if n == 1 {
		return "1 run is waiting for your answers; open it from the table"
	}
	return fmt.Sprintf("%d runs are waiting for your answers; open them from the table", n)
}

func (h *harness) snapshot() harnessState {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.syncBriefs()
	st := h.st
	st.Briefs = append([]harnessBrief(nil), h.st.Briefs...)
	return st
}

// set changes the state under the lock, saves it and wakes the loop.
func (h *harness) set(change func(st *harnessState)) {
	h.mu.Lock()
	change(&h.st)
	h.save()
	h.mu.Unlock()
	go h.tick()
}

func (h *harness) stateHandler(w http.ResponseWriter, r *http.Request) {
	briefJSON(w, http.StatusOK, h.snapshot())
}

func (h *harness) startHandler(w http.ResponseWriter, r *http.Request) {
	h.set(func(st *harnessState) { st.Running, st.Done, st.Note = true, false, "" })
	briefJSON(w, http.StatusOK, h.snapshot())
}

// stopHandler stops the harness from starting runs. A run it already started
// carries on; stop that one from its own page.
func (h *harness) stopHandler(w http.ResponseWriter, r *http.Request) {
	h.set(func(st *harnessState) { st.Running, st.Note = false, "stopped" })
	briefJSON(w, http.StatusOK, h.snapshot())
}

// resetHandler clears the score so every brief has to pass again.
func (h *harness) resetHandler(w http.ResponseWriter, r *http.Request) {
	h.set(func(st *harnessState) {
		for i := range st.Briefs {
			st.Briefs[i] = harnessBrief{File: st.Briefs[i].File, Title: st.Briefs[i].Title, Bytes: st.Briefs[i].Bytes}
		}
		st.Done, st.Next, st.Note = false, 0, "score cleared"
	})
	briefJSON(w, http.StatusOK, h.snapshot())
}
