package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gophermind/gophermind-lib/build/brief"
	"gophermind/gophermind-lib/build/planner"
	"gophermind/gophermind-lib/library"
	"gophermind/gophermind-lib/serve"
)

// `gophermind brief migrate-library` renames the runs the web server made
// under the old gm-YYYY-MM-DD-NNN ids to library names
// (<family>-v1-<yyyymmdd>-rNNN), saves one locked v1 per title in
// <briefs>/library, and writes <briefs>/library/aliases.json so old links
// still resolve. It is a dry run unless --apply is given; --rollback undoes
// an applied or interrupted migration from its journal.

const (
	migrateJournalName = ".migration-journal.jsonl"
	migrateVersion     = "v1"
	migrateLabel       = "imported"
	migrateDefaultURL  = "http://127.0.0.1:8090"
	migrateBackupStem  = "library-migration-backup-"
	migrateMaxBrief    = 256 << 10
)

var (
	oldRunIDRE = regexp.MustCompile(`^gm-([0-9]{4})-([0-9]{2})-([0-9]{2})-[0-9]{3}$`)
	// migrateAfterRename is a test hook run after every rename this
	// invocation makes, with the count so far; an error stops the command.
	migrateAfterRename func(n int) error
	// migrateNow is the clock of the backup stamp.
	migrateNow = func() time.Time { return time.Now().UTC() }
)

type migRun struct {
	Old     string `json:"old"`
	New     string `json:"new"`
	Family  string `json:"family"`
	OldRepo string `json:"old_repo,omitempty"` // repo: line of the old brief
}

type migFamily struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	Text  string `json:"text"` // brief text without id: and repo: lines
}

type migPlan struct {
	Briefs         string      `json:"briefs"`
	Repos          string      `json:"repos"`
	Runs           string      `json:"runs"`
	Backup         string      `json:"backup"`
	LibraryExisted bool        `json:"library_existed"`
	Items          []migRun    `json:"runs_planned"`
	Families       []migFamily `json:"families"`
}

// jEntry is one line of the journal. Every step is written with state begin
// before it happens and done after.
type jEntry struct {
	Op      string   `json:"op"` // plan, backup, lib, rename, rewrite, complete
	State   string   `json:"state,omitempty"`
	From    string   `json:"from,omitempty"`
	To      string   `json:"to,omitempty"`
	Path    string   `json:"path,omitempty"`
	Orig    string   `json:"orig,omitempty"` // backup copy of a rewritten file
	Family  string   `json:"family,omitempty"`
	Existed bool     `json:"existed,omitempty"` // lib: the family dir was already there
	Plan    *migPlan `json:"plan,omitempty"`
}

func (e jEntry) key() string {
	return e.Op + "|" + e.From + "|" + e.To + "|" + e.Path + "|" + e.Family
}

// migRecord mirrors the server's run.json. Decoding refuses unknown fields so
// a rewrite never drops one.
type migRecord struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Phase      string `json:"phase"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at,omitempty"`
	Error      string `json:"error,omitempty"`
	Ask        bool   `json:"ask,omitempty"`
	Family     string `json:"family,omitempty"`
	Version    string `json:"version,omitempty"`
}

func readMigRecord(path string) (migRecord, error) {
	var rec migRecord
	b, err := os.ReadFile(path)
	if err != nil {
		return rec, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rec); err != nil {
		return rec, fmt.Errorf("%s: %w", path, err)
	}
	return rec, nil
}

type migrator struct {
	briefs, repos, runs, serverURL string
	out, errw                      io.Writer

	libDir  string
	jpath   string
	journal *os.File
	plan    *migPlan
	done    map[string]bool // steps journaled done
	begun   map[string]bool // steps journaled begin
	renames int
}

func briefMigrateLibrary(args []string, out, errw io.Writer) int {
	fset := flag.NewFlagSet("migrate-library", flag.ContinueOnError)
	fset.SetOutput(errw)
	briefs := fset.String("briefs", "", "the server's briefs dir")
	repos := fset.String("repos", "", "the server's per-run repos dir")
	runs := fset.String("runs", "", "the run records dir (<config dir>/runs)")
	url := fset.String("server-url", migrateDefaultURL, "the server to check is stopped")
	apply := fset.Bool("apply", false, "migrate (default is a dry run)")
	rollback := fset.Bool("rollback", false, "undo a migration from its journal")
	if err := fset.Parse(args); err != nil || fset.NArg() != 0 || *briefs == "" || *repos == "" || *runs == "" || (*apply && *rollback) {
		fmt.Fprintln(errw, "usage: gophermind brief migrate-library --briefs DIR --repos DIR --runs DIR [--server-url URL] [--apply | --rollback]")
		return 1
	}
	m := &migrator{serverURL: strings.TrimRight(*url, "/"), out: out, errw: errw,
		done: map[string]bool{}, begun: map[string]bool{}}
	for _, p := range []struct {
		dst *string
		src string
	}{{&m.briefs, *briefs}, {&m.repos, *repos}, {&m.runs, *runs}} {
		abs, err := filepath.Abs(p.src)
		if err != nil {
			fmt.Fprintln(errw, "gophermind brief migrate-library:", err)
			return 1
		}
		*p.dst = filepath.Clean(abs)
	}
	m.libDir = filepath.Join(m.briefs, "library")
	m.jpath = filepath.Join(m.libDir, migrateJournalName)
	var err error
	switch {
	case *rollback:
		err = m.rollback()
	case *apply:
		err = m.apply()
	default:
		err = m.dryRun()
	}
	if m.journal != nil {
		m.journal.Close()
	}
	if err != nil {
		fmt.Fprintln(errw, "gophermind brief migrate-library:", err)
		return 1
	}
	return 0
}

// ---- planning (read only) ----

// frontLine returns the value of key: inside the frontmatter of src.
func frontLine(src []byte, key string) string {
	lines := strings.Split(string(src), "\n")
	if len(lines) == 0 || strings.TrimSuffix(lines[0], "\r") != "---" {
		return ""
	}
	for _, l := range lines[1:] {
		l = strings.TrimSuffix(l, "\r")
		if l == "---" {
			break
		}
		if v, ok := strings.CutPrefix(l, key+":"); ok {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

func briefTitle(src []byte) string {
	if b, err := brief.Parse(src); err == nil && strings.TrimSpace(b.Front.Title) != "" {
		return strings.TrimSpace(b.Front.Title)
	}
	return frontLine(src, "title")
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// buildPlan reads the tree and works out every rename. It changes nothing.
// problems are reasons --apply must refuse; skipped are run records left alone.
func (m *migrator) buildPlan() (plan *migPlan, skipped, problems []string, err error) {
	plan = &migPlan{Briefs: m.briefs, Repos: m.repos, Runs: m.runs}
	ents, err := os.ReadDir(m.briefs)
	if err != nil {
		return nil, nil, nil, err
	}
	type found struct {
		run   migRun
		title string
		text  string
	}
	var all []found
	for _, e := range ents {
		old := e.Name()
		if !oldRunIDRE.MatchString(old) {
			continue
		}
		if !e.IsDir() {
			problems = append(problems, fmt.Sprintf("%s: not a directory", filepath.Join(m.briefs, old)))
			continue
		}
		dir := filepath.Join(m.briefs, old)
		src, err := os.ReadFile(filepath.Join(dir, "brief.md"))
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: no readable brief.md: %v", dir, err))
			continue
		}
		if len(src) > migrateMaxBrief {
			problems = append(problems, fmt.Sprintf("%s: brief.md is over %d bytes", dir, migrateMaxBrief))
			continue
		}
		title := briefTitle(src)
		if title == "" {
			problems = append(problems, fmt.Sprintf("%s: brief.md has no title", dir))
			continue
		}
		fam, err := library.Slug(title)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: title %q: %v", dir, title, err))
			continue
		}
		rec, err := readMigRecord(filepath.Join(dir, "run.json"))
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: run.json: %v", dir, err))
			continue
		}
		if rec.ID != old {
			problems = append(problems, fmt.Sprintf("%s: run.json names id %q", dir, rec.ID))
			continue
		}
		if id := frontLine(src, "id"); id != "" && id != old {
			problems = append(problems, fmt.Sprintf("%s: brief.md names id %q", dir, id))
			continue
		}
		all = append(all, found{run: migRun{Old: old, Family: fam, OldRepo: frontLine(src, "repo")},
			title: title, text: string(serve.StripStamp(src))})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].run.Old < all[j].run.Old })

	// Families: one text per family, checked rather than assumed.
	byFam := map[string][]found{}
	var famNames []string
	for _, f := range all {
		if _, ok := byFam[f.run.Family]; !ok {
			famNames = append(famNames, f.run.Family)
		}
		byFam[f.run.Family] = append(byFam[f.run.Family], f)
	}
	sort.Strings(famNames)
	for _, name := range famNames {
		group := byFam[name]
		variants := map[string][]string{}
		var order []string
		for _, f := range group {
			if _, ok := variants[f.text]; !ok {
				order = append(order, f.text)
			}
			variants[f.text] = append(variants[f.text], filepath.Join(m.briefs, f.run.Old))
		}
		if len(order) > 1 {
			var b strings.Builder
			fmt.Fprintf(&b, "family %s: the brief text differs between folders once id: and repo: are removed:", name)
			for i, t := range order {
				fmt.Fprintf(&b, "\n    variant %d: %s", i+1, strings.Join(variants[t], " "))
			}
			problems = append(problems, b.String())
			continue
		}
		if len(group[0].text) > library.MaxTextBytes {
			problems = append(problems, fmt.Sprintf("family %s: brief text is over the library limit", name))
			continue
		}
		plan.Families = append(plan.Families, migFamily{Name: name, Title: group[0].title, Text: group[0].text})
	}

	// New ids: per family, version and date, in old-id order.
	counter := map[string]int{}
	seen := map[string]bool{}
	for _, f := range all {
		r := f.run
		sm := oldRunIDRE.FindStringSubmatch(r.Old)
		day, err := time.Parse("2006-01-02", sm[1]+"-"+sm[2]+"-"+sm[3])
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: bad date in the id", r.Old))
			continue
		}
		k := r.Family + "|" + day.Format("20060102")
		counter[k]++
		r.New = library.RunName(r.Family, migrateVersion, day, counter[k])
		switch {
		case !planner.ValidRunID(r.New):
			problems = append(problems, fmt.Sprintf("%s: new id %q is not a valid run id", r.Old, r.New))
		case seen[r.New]:
			problems = append(problems, fmt.Sprintf("%s: new id %q is planned twice", r.Old, r.New))
		case exists(filepath.Join(m.briefs, r.New)):
			problems = append(problems, fmt.Sprintf("%s: %s already exists", r.Old, filepath.Join(m.briefs, r.New)))
		case exists(filepath.Join(m.repos, r.New)):
			problems = append(problems, fmt.Sprintf("%s: %s already exists", r.Old, filepath.Join(m.repos, r.New)))
		case exists(filepath.Join(m.runs, r.New+".json")):
			problems = append(problems, fmt.Sprintf("%s: %s already exists", r.Old, filepath.Join(m.runs, r.New+".json")))
		}
		seen[r.New] = true
		plan.Items = append(plan.Items, r)
	}

	// Run records with no briefs folder are left alone.
	inPlan := map[string]bool{}
	for _, r := range plan.Items {
		inPlan[r.Old] = true
	}
	rents, err := os.ReadDir(m.runs)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil, err
	}
	for _, e := range rents {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if ok && oldRunIDRE.MatchString(id) && !inPlan[id] {
			skipped = append(skipped, filepath.Join(m.runs, e.Name()))
		}
	}
	// The backup must be whole: anything it could not copy is refused now.
	if len(plan.Items) > 0 {
		problems = append(problems, uncopyable(m.briefs, "library")...)
		if exists(m.repos) {
			problems = append(problems, uncopyable(m.repos, "")...)
		}
	}
	return plan, skipped, problems, nil
}

// uncopyable lists what copyTree could not back up under root: unreadable
// files and directories, and entries that are not a file, directory or link.
func uncopyable(root, skipTop string) []string {
	var bad []string
	n := 0
	note := func(s string) {
		n++
		if len(bad) < 20 {
			bad = append(bad, "the backup cannot copy "+s)
		}
	}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			note(p + ": " + err.Error())
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if skipTop != "" && p == filepath.Join(root, skipTop) && d.IsDir() {
			return filepath.SkipDir
		}
		switch t := d.Type(); {
		case t.IsDir(), t&fs.ModeSymlink != 0:
		case t.IsRegular():
			f, err := os.Open(p)
			if err != nil {
				note(p + ": " + err.Error())
				return nil
			}
			f.Close()
		default:
			note(p + ": not a file, directory or link")
		}
		return nil
	})
	if n > len(bad) {
		bad = append(bad, fmt.Sprintf("the backup cannot copy %d more entries under %s", n-len(bad), root))
	}
	return bad
}

// liveRuns lists runs whose run.json says planning, running or waiting.
func (m *migrator) liveRuns() []string {
	var live []string
	ents, _ := os.ReadDir(m.briefs)
	for _, e := range ents {
		if !e.IsDir() || !planner.ValidRunID(e.Name()) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(m.briefs, e.Name(), "run.json"))
		if err != nil {
			continue
		}
		var r struct {
			Phase string `json:"phase"`
		}
		_ = json.Unmarshal(b, &r)
		switch r.Phase {
		case "planning", "running", "waiting":
			live = append(live, e.Name()+" ("+r.Phase+")")
		}
	}
	return live
}

func (m *migrator) serverAnswers() bool {
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(m.serverURL + "/healthz")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

// libraryHasVersions reports whether the library dir holds any version. It
// does not create the dir.
func (m *migrator) libraryHasVersions() (bool, error) {
	if !exists(m.libDir) {
		return false, nil
	}
	st, err := library.Open(m.libDir)
	if err != nil {
		return false, err
	}
	fams, err := st.List()
	return len(fams) > 0, err
}

func (m *migrator) printPlan(plan *migPlan, skipped []string) {
	for _, r := range plan.Items {
		fmt.Fprintf(m.out, "%s -> %s\n", r.Old, r.New)
	}
	for _, f := range plan.Families {
		n := 0
		first := ""
		for _, r := range plan.Items {
			if r.Family == f.Name {
				if first == "" {
					first = r.Old
				}
				n++
			}
		}
		fmt.Fprintf(m.out, "family %s: %d runs, v1 (locked, label %q) from %s\n", f.Name, n, migrateLabel, first)
	}
	for _, s := range skipped {
		fmt.Fprintf(m.out, "skipped %s (no briefs folder)\n", s)
	}
}

func (m *migrator) dryRun() error {
	if exists(m.jpath) {
		fmt.Fprintf(m.out, "a migration journal exists at %s: --apply resumes it, --rollback undoes it\n", m.jpath)
		return nil
	}
	plan, skipped, problems, err := m.buildPlan()
	if err != nil {
		return err
	}
	m.printPlan(plan, skipped)
	if has, err := m.libraryHasVersions(); err != nil || has {
		problems = append(problems, fmt.Sprintf("%s already holds versions (already migrated?)", m.libDir))
	}
	for _, l := range m.liveRuns() {
		problems = append(problems, "run is live: "+l)
	}
	for _, p := range problems {
		fmt.Fprintln(m.out, "problem:", p)
	}
	fmt.Fprintf(m.out, "dry run: %d renames across %d families, %d skipped, %d problems; nothing changed\n",
		len(plan.Items), len(plan.Families), len(skipped), len(problems))
	if len(problems) > 0 {
		return fmt.Errorf("--apply would refuse: %d problems", len(problems))
	}
	return nil
}

// ---- journal ----

func (m *migrator) record(e jEntry) error {
	if m.journal == nil {
		f, err := os.OpenFile(m.jpath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		m.journal = f
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if _, err := m.journal.Write(append(b, '\n')); err != nil {
		return err
	}
	if err := m.journal.Sync(); err != nil {
		return err
	}
	switch e.State {
	case "begin":
		m.begun[e.key()] = true
	case "done":
		m.done[e.key()] = true
	}
	return nil
}

// readJournal reads every entry; a torn last line (a crash mid-write) is ignored.
func readJournal(path string) ([]jEntry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []jEntry
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	for i, l := range lines {
		var e jEntry
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			if i == len(lines)-1 && !bytes.HasSuffix(b, []byte("\n")) {
				break
			}
			return nil, fmt.Errorf("journal line %d: %w", i+1, err)
		}
		out = append(out, e)
	}
	if len(out) == 0 || out[0].Op != "plan" || out[0].Plan == nil {
		return nil, errors.New("journal does not start with a plan")
	}
	return out, nil
}

// ---- apply ----

func (m *migrator) preflight() error {
	if m.serverAnswers() {
		return fmt.Errorf("the server at %s answers /healthz: stop it first", m.serverURL)
	}
	if live := m.liveRuns(); len(live) > 0 {
		return fmt.Errorf("runs are live, wait for them or stop them first: %s", strings.Join(live, ", "))
	}
	return nil
}

func (m *migrator) apply() error {
	if err := m.preflight(); err != nil {
		return err
	}
	if exists(m.jpath) {
		entries, err := readJournal(m.jpath)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.Op == "complete" {
				return fmt.Errorf("already migrated (the journal %s says complete); --rollback undoes it", m.jpath)
			}
			switch e.State {
			case "begin":
				m.begun[e.key()] = true
			case "done":
				m.done[e.key()] = true
			}
		}
		m.plan = entries[0].Plan
		if m.plan.Briefs != m.briefs || m.plan.Repos != m.repos || m.plan.Runs != m.runs {
			return fmt.Errorf("the journal was written for --briefs %s --repos %s --runs %s", m.plan.Briefs, m.plan.Repos, m.plan.Runs)
		}
		fmt.Fprintf(m.out, "resuming the migration in %s\n", m.jpath)
	} else {
		if has, err := m.libraryHasVersions(); err != nil {
			return err
		} else if has {
			return fmt.Errorf("already migrated: %s already holds versions", m.libDir)
		}
		if exists(filepath.Join(m.libDir, library.AliasesFile)) {
			return fmt.Errorf("already migrated: %s exists", filepath.Join(m.libDir, library.AliasesFile))
		}
		plan, skipped, problems, err := m.buildPlan()
		if err != nil {
			return err
		}
		m.printPlan(plan, skipped)
		if len(problems) > 0 {
			for _, p := range problems {
				fmt.Fprintln(m.out, "problem:", p)
			}
			return fmt.Errorf("refusing, nothing changed: %d problems", len(problems))
		}
		if len(plan.Items) == 0 {
			fmt.Fprintln(m.out, "nothing to migrate")
			return nil
		}
		plan.Backup = filepath.Join(filepath.Dir(m.briefs), migrateBackupStem+migrateNow().Format("20060102T150405Z"))
		if exists(plan.Backup) {
			return fmt.Errorf("backup destination %s already exists", plan.Backup)
		}
		for _, d := range []string{m.briefs, m.repos, m.runs} {
			if strings.HasPrefix(plan.Backup+string(filepath.Separator), d+string(filepath.Separator)) {
				return fmt.Errorf("backup destination %s would be inside %s", plan.Backup, d)
			}
		}
		if err := m.checkSpace(plan); err != nil {
			return err
		}
		plan.LibraryExisted = exists(m.libDir)
		if err := os.MkdirAll(m.libDir, 0o755); err != nil {
			return err
		}
		m.plan = plan
		if err := m.record(jEntry{Op: "plan", Plan: plan}); err != nil {
			return err
		}
	}
	if err := m.backup(); err != nil {
		return err
	}
	if err := m.saveFamilies(); err != nil {
		return err
	}
	aliases, err := library.LoadAliases(m.libDir)
	if err != nil {
		return err
	}
	for _, r := range m.plan.Items {
		if err := m.migrateRun(r); err != nil {
			return fmt.Errorf("%s -> %s: %w (rerun --apply to resume, or --rollback)", r.Old, r.New, err)
		}
		aliases[r.Old] = r.New
		if err := library.SaveAliases(m.libDir, aliases); err != nil {
			return err
		}
	}
	all := library.Aliases{}
	for _, r := range m.plan.Items {
		all[r.Old] = r.New
	}
	if err := library.SaveAliases(m.libDir, all); err != nil {
		return err
	}
	if err := m.record(jEntry{Op: "complete"}); err != nil {
		return err
	}
	return m.verify()
}

func (m *migrator) checkSpace(plan *migPlan) error {
	need, err := treeSize(m.briefs, "library")
	if err != nil {
		return err
	}
	if exists(m.repos) {
		n, err := treeSize(m.repos, "")
		if err != nil {
			return err
		}
		need += n
	}
	for _, r := range plan.Items {
		if fi, err := os.Lstat(filepath.Join(m.runs, r.Old+".json")); err == nil {
			need += fi.Size()
		}
	}
	free, err := freeBytes(filepath.Dir(m.briefs))
	if err != nil {
		fmt.Fprintf(m.out, "free space could not be checked: %v\n", err)
		return nil
	}
	want := need + need/10 + 64<<20
	if free < uint64(want) {
		return fmt.Errorf("free space looks too small for the backup: %d bytes free, want %d", free, want)
	}
	return nil
}

func treeSize(root, skipTop string) (int64, error) {
	var n int64
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if skipTop != "" && d.IsDir() && p == filepath.Join(root, skipTop) {
			return filepath.SkipDir
		}
		if d.Type().IsRegular() {
			fi, err := d.Info()
			if err != nil {
				return err
			}
			n += fi.Size()
		}
		return nil
	})
	return n, err
}

// copyTree copies src to dst keeping modes and symlinks. skipTop names a
// top-level entry of src to leave out.
func copyTree(src, dst, skipTop string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if skipTop != "" && p == filepath.Join(src, skipTop) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		switch {
		case fi.IsDir():
			return os.MkdirAll(target, fi.Mode().Perm()|0o700)
		case fi.Mode()&fs.ModeSymlink != 0:
			l, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(l, target)
		case fi.Mode().IsRegular():
			return copyFile(p, target, fi.Mode().Perm())
		default:
			return fmt.Errorf("%s: not a file, directory or link; not copying", p)
		}
	})
}

func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func (m *migrator) backup() error {
	k := jEntry{Op: "backup", Path: m.plan.Backup}
	if m.done[k.key()] {
		return nil
	}
	if m.begun[k.key()] {
		return fmt.Errorf("the backup into %s was interrupted; inspect it, run --rollback, then --apply again", m.plan.Backup)
	}
	k.State = "begin"
	if err := m.record(k); err != nil {
		return err
	}
	fmt.Fprintf(m.out, "backing up to %s\n", m.plan.Backup)
	if err := os.Mkdir(m.plan.Backup, 0o700); err != nil {
		return err
	}
	if err := copyTree(m.briefs, filepath.Join(m.plan.Backup, "briefs"), "library"); err != nil {
		return err
	}
	if exists(m.repos) {
		if err := copyTree(m.repos, filepath.Join(m.plan.Backup, "repos"), ""); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join(m.plan.Backup, "runs"), 0o700); err != nil {
		return err
	}
	for _, r := range m.plan.Items {
		p := filepath.Join(m.runs, r.Old+".json")
		if fi, err := os.Lstat(p); err == nil && fi.Mode().IsRegular() {
			if err := copyFile(p, filepath.Join(m.plan.Backup, "runs", r.Old+".json"), fi.Mode().Perm()); err != nil {
				return err
			}
		}
	}
	k.State = "done"
	return m.record(k)
}

func (m *migrator) saveFamilies() error {
	st, err := library.Open(m.libDir)
	if err != nil {
		return err
	}
	for _, f := range m.plan.Families {
		k := jEntry{Op: "lib", Family: f.Name}
		if m.done[k.key()] {
			continue
		}
		_, text, gerr := st.Get(f.Name, migrateVersion)
		switch {
		case gerr == nil && string(text) != f.Text:
			return fmt.Errorf("library family %s already has a different v1", f.Name)
		case gerr == nil && !m.begun[k.key()]:
			return fmt.Errorf("library family %s already has a v1 this migration did not save", f.Name)
		case gerr != nil && !errors.Is(gerr, library.ErrNoSuchVersion):
			return gerr
		case gerr != nil:
			k.State, k.Existed = "begin", exists(filepath.Join(m.libDir, f.Name))
			if err := m.record(k); err != nil {
				return err
			}
			v, err := st.Save(f.Name, "", migrateLabel, []byte(f.Text))
			if err != nil {
				return fmt.Errorf("save family %s: %w", f.Name, err)
			}
			if v.Name != migrateVersion {
				return fmt.Errorf("family %s saved as %s, not %s", f.Name, v.Name, migrateVersion)
			}
		}
		if err := st.Lock(f.Name, migrateVersion); err != nil {
			return fmt.Errorf("lock family %s: %w", f.Name, err)
		}
		k.State, k.Existed = "done", false
		if err := m.record(k); err != nil {
			return err
		}
	}
	return nil
}

// renameStep renames from to to once. A step the journal began whose target
// exists and source does not finished before a crash: it is marked done.
func (m *migrator) renameStep(from, to string, required bool) error {
	k := jEntry{Op: "rename", From: from, To: to}
	if m.done[k.key()] {
		return nil
	}
	fromOK, toOK := exists(from), exists(to)
	switch {
	case fromOK && !toOK:
		k.State = "begin"
		if err := m.record(k); err != nil {
			return err
		}
		if err := os.Rename(from, to); err != nil {
			return err
		}
		k.State = "done"
		if err := m.record(k); err != nil {
			return err
		}
		m.renames++
		if migrateAfterRename != nil {
			return migrateAfterRename(m.renames)
		}
		return nil
	case !fromOK && toOK && m.begun[k.key()]:
		k.State = "done"
		return m.record(k)
	case !fromOK && !toOK && !required:
		return nil
	case fromOK && toOK:
		return fmt.Errorf("both %s and %s exist", from, to)
	case !fromOK && toOK:
		return fmt.Errorf("%s exists but the journal has no record of renaming %s to it", to, from)
	default:
		return fmt.Errorf("%s is missing", from)
	}
}

// replacer turns old references of one run into new ones.
type replacer struct {
	oldID, newID string
	paths        [][2]string // old path, new path; longest first
}

func (m *migrator) replacerFor(r migRun) replacer {
	rp := replacer{oldID: r.Old, newID: r.New}
	add := func(oldRepo string) {
		if oldRepo == "" || filepath.Base(oldRepo) != r.Old {
			return
		}
		newRepo := filepath.Join(filepath.Dir(oldRepo), r.New)
		for _, p := range rp.paths {
			if p[0] == oldRepo {
				return
			}
		}
		rp.paths = append(rp.paths,
			[2]string{oldRepo + "/.gophermind/" + r.Old, newRepo + "/.gophermind/" + r.New},
			[2]string{oldRepo, newRepo})
	}
	add(r.OldRepo)
	add(filepath.Join(m.repos, r.Old))
	sort.SliceStable(rp.paths, func(i, j int) bool { return len(rp.paths[i][0]) > len(rp.paths[j][0]) })
	return rp
}

const (
	modeFull = iota // paths, then every occurrence of the old id
	modeJSON        // paths, then the old id as a whole quoted JSON string
	modePath        // paths only
)

func (rp replacer) apply(b []byte, mode int) []byte {
	for _, p := range rp.paths {
		b = bytes.ReplaceAll(b, []byte(p[0]), []byte(p[1]))
	}
	switch mode {
	case modeFull:
		b = bytes.ReplaceAll(b, []byte(rp.oldID), []byte(rp.newID))
	case modeJSON:
		b = bytes.ReplaceAll(b, []byte(`"`+rp.oldID+`"`), []byte(`"`+rp.newID+`"`))
	}
	return b
}

// writeKeep writes data to path through a temp file and a rename, keeping perm.
func writeKeep(path string, data []byte, perm fs.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".migrate-tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// rewriteStep rewrites path with fn. orig is the backup copy of the file as
// it was before the migration, which --rollback writes back.
func (m *migrator) rewriteStep(path, orig string, fn func([]byte) ([]byte, error)) error {
	k := jEntry{Op: "rewrite", Path: path}
	if m.done[k.key()] {
		return nil
	}
	fi, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if !fi.Mode().IsRegular() {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	nd, err := fn(data)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if bytes.Equal(nd, data) {
		return nil
	}
	if ofi, err := os.Lstat(orig); err != nil || !ofi.Mode().IsRegular() {
		return fmt.Errorf("%s: no backup copy at %s", path, orig)
	}
	k.State, k.Orig = "begin", orig
	if err := m.record(k); err != nil {
		return err
	}
	if err := writeKeep(path, nd, fi.Mode().Perm()); err != nil {
		return err
	}
	k.State = "done"
	return m.record(k)
}

func (m *migrator) migrateRun(r migRun) error {
	newB := filepath.Join(m.briefs, r.New)
	if err := m.renameStep(filepath.Join(m.briefs, r.Old), newB, true); err != nil {
		return err
	}
	newRepo := filepath.Join(m.repos, r.New)
	if err := m.renameStep(filepath.Join(m.repos, r.Old), newRepo, false); err != nil {
		return err
	}
	newRunDir := filepath.Join(newRepo, ".gophermind", r.New)
	if exists(newRepo) {
		if err := m.renameStep(filepath.Join(newRepo, ".gophermind", r.Old), newRunDir, false); err != nil {
			return err
		}
	}
	if err := m.renameStep(filepath.Join(m.runs, r.Old+".json"), filepath.Join(m.runs, r.New+".json"), false); err != nil {
		return err
	}

	rp := m.replacerFor(r)
	bk := m.plan.Backup
	full := func(b []byte) ([]byte, error) { return rp.apply(b, modeFull), nil }
	jsonOnly := func(b []byte) ([]byte, error) { return rp.apply(b, modeJSON), nil }
	pathOnly := func(b []byte) ([]byte, error) { return rp.apply(b, modePath), nil }

	if err := m.rewriteStep(filepath.Join(newB, "brief.md"), filepath.Join(bk, "briefs", r.Old, "brief.md"), full); err != nil {
		return err
	}
	if err := m.rewriteStep(filepath.Join(newB, "run.json"), filepath.Join(bk, "briefs", r.Old, "run.json"), func(b []byte) ([]byte, error) {
		var rec migRecord
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&rec); err != nil {
			return nil, err
		}
		rec.ID, rec.Family, rec.Version = r.New, r.Family, migrateVersion
		out, err := json.MarshalIndent(rec, "", "  ")
		return append(out, '\n'), err
	}); err != nil {
		return err
	}

	// The run folder in the repo: brief.md and calls.jsonl get every mention;
	// other JSON files get the repo path and the id where it is a whole JSON
	// string (node ids, brief_id). logs/ and non-JSON text stay as written.
	if fi, err := os.Lstat(newRunDir); err == nil && fi.IsDir() {
		origDir := filepath.Join(bk, "repos", r.Old, ".gophermind", r.Old)
		err := filepath.WalkDir(newRunDir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(newRunDir, p)
			if d.IsDir() {
				if rel == "logs" {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() || strings.HasPrefix(d.Name(), ".migrate-tmp-") {
				return nil
			}
			slash := filepath.ToSlash(rel)
			fn := jsonOnly
			switch {
			case slash == "brief.md" || slash == "_state/calls.jsonl":
				fn = full
			case strings.HasSuffix(slash, ".json") || strings.HasSuffix(slash, ".jsonl"):
			default:
				return nil
			}
			return m.rewriteStep(p, filepath.Join(origDir, rel), fn)
		})
		if err != nil {
			return err
		}
	}

	// git: a renamed working copy only breaks if git stored its absolute
	// path. Rewrite that path in .git/config, a .git file and worktree gitdir
	// files; nothing else in .git is touched.
	gitPath := filepath.Join(newRepo, ".git")
	origGit := filepath.Join(bk, "repos", r.Old, ".git")
	if fi, err := os.Lstat(gitPath); err == nil {
		if fi.Mode().IsRegular() {
			if err := m.rewriteStep(gitPath, origGit, pathOnly); err != nil {
				return err
			}
		} else if fi.IsDir() {
			if err := m.rewriteStep(filepath.Join(gitPath, "config"), filepath.Join(origGit, "config"), pathOnly); err != nil {
				return err
			}
			wts, _ := os.ReadDir(filepath.Join(gitPath, "worktrees"))
			for _, w := range wts {
				rel := filepath.Join("worktrees", w.Name(), "gitdir")
				if err := m.rewriteStep(filepath.Join(gitPath, rel), filepath.Join(origGit, rel), pathOnly); err != nil {
					return err
				}
			}
		}
	}

	return m.rewriteStep(filepath.Join(m.runs, r.New+".json"), filepath.Join(bk, "runs", r.Old+".json"), jsonOnly)
}

// verify loads every migrated run the way the server does and prints counts.
func (m *migrator) verify() error {
	st, err := library.Open(m.libDir)
	if err != nil {
		return err
	}
	var errs []string
	for _, f := range m.plan.Families {
		v, _, err := st.Get(f.Name, migrateVersion)
		if err != nil || !v.Locked {
			errs = append(errs, fmt.Sprintf("family %s: v1 missing or not locked", f.Name))
		}
	}
	aliases, err := library.LoadAliases(m.libDir)
	if err != nil {
		errs = append(errs, "aliases: "+err.Error())
	}
	for _, r := range m.plan.Items {
		if err := serve.CheckRun(m.briefs, m.repos, r.New); err != nil {
			errs = append(errs, err.Error())
		}
		if aliases.Resolve(r.Old) != r.New {
			errs = append(errs, r.Old+": alias missing")
		}
		if exists(filepath.Join(m.briefs, r.Old)) || exists(filepath.Join(m.repos, r.Old)) {
			errs = append(errs, r.Old+": the old folder is still there")
		}
	}
	skipped := 0
	if rents, err := os.ReadDir(m.runs); err == nil {
		for _, e := range rents {
			if id, ok := strings.CutSuffix(e.Name(), ".json"); ok && oldRunIDRE.MatchString(id) {
				skipped++
				fmt.Fprintf(m.out, "skipped %s (no briefs folder)\n", filepath.Join(m.runs, e.Name()))
			}
		}
	}
	for _, e := range errs {
		fmt.Fprintln(m.out, "error:", e)
	}
	fmt.Fprintf(m.out, "migrated: families %d, runs migrated %d, skipped %d, errors %d; backup kept at %s\n",
		len(m.plan.Families), len(m.plan.Items), skipped, len(errs), m.plan.Backup)
	if len(errs) > 0 {
		return fmt.Errorf("%d runs or families did not check out", len(errs))
	}
	return nil
}

// ---- rollback ----

// guardedRemove deletes target, which must be an absolute path strictly
// inside base. It prints what it removes. recursive=false removes one file or
// an empty directory.
func (m *migrator) guardedRemove(base, target string, recursive bool) error {
	target = filepath.Clean(target)
	base = filepath.Clean(base)
	if target == "" || base == "" || !filepath.IsAbs(target) || !filepath.IsAbs(base) ||
		!strings.HasPrefix(target, base+string(filepath.Separator)) {
		return fmt.Errorf("refusing to remove %q: not inside %q", target, base)
	}
	if !exists(target) {
		return nil
	}
	fmt.Fprintf(m.out, "removing %s\n", target)
	if recursive {
		return os.RemoveAll(target)
	}
	return os.Remove(target)
}

func (m *migrator) rollback() error {
	if err := m.preflight(); err != nil {
		return err
	}
	if !exists(m.jpath) {
		return fmt.Errorf("no migration journal at %s: nothing to roll back", m.jpath)
	}
	entries, err := readJournal(m.jpath)
	if err != nil {
		return err
	}
	plan := entries[0].Plan
	if plan.Briefs != m.briefs || plan.Repos != m.repos || plan.Runs != m.runs {
		return fmt.Errorf("the journal was written for --briefs %s --repos %s --runs %s", plan.Briefs, plan.Repos, plan.Runs)
	}
	handled := map[string]bool{}
	undone := 0
	for i := len(entries) - 1; i >= 1; i-- {
		e := entries[i]
		if e.State == "" || handled[e.key()] {
			continue
		}
		handled[e.key()] = true
		switch e.Op {
		case "rename":
			fromOK, toOK := exists(e.From), exists(e.To)
			switch {
			case toOK && !fromOK:
				if err := os.Rename(e.To, e.From); err != nil {
					return err
				}
				undone++
			case fromOK && !toOK:
				// never happened, or already undone
			default:
				return fmt.Errorf("cannot undo the rename of %s to %s: both or neither exist", e.From, e.To)
			}
		case "rewrite":
			if _, err := os.Lstat(e.Path); err != nil {
				continue // its folder was already renamed back
			}
			ofi, err := os.Lstat(e.Orig)
			if err != nil {
				return fmt.Errorf("cannot restore %s: backup copy %q: %w", e.Path, e.Orig, err)
			}
			data, err := os.ReadFile(e.Orig)
			if err != nil {
				return err
			}
			if err := writeKeep(e.Path, data, ofi.Mode().Perm()); err != nil {
				return err
			}
			undone++
		case "lib":
			existed := e.Existed
			for _, b := range entries {
				if b.Op == "lib" && b.Family == e.Family && b.State == "begin" {
					existed = b.Existed
				}
			}
			target := filepath.Join(m.libDir, e.Family)
			if existed {
				target = filepath.Join(target, migrateVersion)
			}
			if err := m.guardedRemove(m.libDir, target, true); err != nil {
				return err
			}
			undone++
		}
	}
	if err := m.guardedRemove(m.libDir, filepath.Join(m.libDir, library.AliasesFile), false); err != nil {
		return err
	}
	if err := m.guardedRemove(m.libDir, m.jpath, false); err != nil {
		return err
	}
	if !plan.LibraryExisted {
		if err := m.guardedRemove(m.briefs, m.libDir, false); err != nil {
			return fmt.Errorf("library dir not removed (not empty?): %w", err)
		}
	}
	fmt.Fprintf(m.out, "rolled back %d steps; backup kept at %s\n", undone, plan.Backup)
	return nil
}
