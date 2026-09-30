package planner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/rundir"
	"gophermind/gophermind-lib/briefv2/vault"
)

var runIDRE = regexp.MustCompile(`^gm-[0-9]{4}-[0-9]{2}-[0-9]{2}-[0-9]{3}$`)

// load is the Load stage. For `plan` it validates the brief, makes sure its
// secrets are in the vault, creates the run folder and writes the
// requirements. For `resume` it finds the run and reads the same things back.
func (p *Planner) load(ctx context.Context, o Options) (*run, error) {
	if o.RunID != "" {
		// A run id and a brief path together must name the same brief.
		if o.BriefPath != "" {
			rec, err := LookupRun(o.RunID)
			if err != nil {
				return nil, err
			}
			abs, aerr := filepath.Abs(o.BriefPath)
			if aerr != nil {
				abs = o.BriefPath
			}
			if abs != rec.BriefPath {
				return nil, fmt.Errorf("planner: run %s was planned from %s, not %s; give a run id or a brief file, not both", o.RunID, rec.BriefPath, abs)
			}
		}
		return p.loadExisting(o)
	}
	if o.BriefPath == "" {
		return nil, errors.New("planner: give a brief file to plan or a run id to resume")
	}
	p.emit(events.KindStageStarted, "load", "", "")
	src, err := os.ReadFile(o.BriefPath)
	if err != nil {
		return nil, fmt.Errorf("planner: load: %w", err)
	}
	b, err := brief.Parse(src)
	if err != nil {
		return nil, err
	}
	repo, err := resolveRepo(b.Front.Repo)
	if err != nil {
		return nil, err
	}
	// Secrets come before the run folder: a missing secret must not leave a
	// half-made run behind.
	if err := p.storeSecrets(b); err != nil {
		return nil, fmt.Errorf("planner: load: %w", err)
	}
	dir, err := rundir.Create(repo, b.Front.ID, src)
	if errors.Is(err, rundir.ErrExists) {
		return nil, fmt.Errorf("planner: run %s already exists in %s; continue it with `gophermind brief resume %s`", b.Front.ID, repo, b.Front.ID)
	}
	if err != nil {
		return nil, fmt.Errorf("planner: load: %w", err)
	}
	briefPath, err := filepath.Abs(o.BriefPath)
	if err != nil {
		briefPath = o.BriefPath
	}
	recPath, err := runRecordPath(b.Front.ID)
	if err != nil {
		return nil, fmt.Errorf("planner: load: %w", err)
	}
	rec := RunRecord{RunID: b.Front.ID, Repo: repo, RunDir: dir, BriefPath: briefPath, StartedAt: p.d.Now().UTC().Format("2006-01-02T15:04:05Z")}
	if err := writeJSON(recPath, rec); err != nil {
		return nil, fmt.Errorf("planner: load: %w", err)
	}
	r := &run{id: b.Front.ID, dir: dir, repo: repo, src: src, brief: b, reqs: ParseRequirements(src), opts: o}
	if err := writeJSON(r.path(fileRequirements), r.reqs); err != nil {
		return nil, fmt.Errorf("planner: load: %w", err)
	}
	if err := p.notePublic(r, o); err != nil {
		return nil, err
	}
	p.emit(events.KindStageFinished, "load", "", "")
	return r, nil
}

func (p *Planner) loadExisting(o Options) (*run, error) {
	rec, err := LookupRun(o.RunID)
	if err != nil {
		return nil, err
	}
	src, err := os.ReadFile(filepath.Join(rec.RunDir, fileBrief))
	if err != nil {
		return nil, fmt.Errorf("planner: run %s: %w", o.RunID, err)
	}
	b, err := brief.Parse(src)
	if err != nil {
		return nil, err
	}
	if err := p.storeSecrets(b); err != nil {
		return nil, fmt.Errorf("planner: load: %w", err)
	}
	r := &run{id: rec.RunID, dir: rec.RunDir, repo: rec.Repo, src: src, brief: b, reqs: ParseRequirements(src), opts: o}
	if _, err := readJSON(r.path(stateStatus), &r.status); err != nil {
		return nil, err
	}
	if !exists(r.path(fileRequirements)) {
		if err := writeJSON(r.path(fileRequirements), r.reqs); err != nil {
			return nil, err
		}
	}
	if err := p.notePublic(r, o); err != nil {
		return nil, err
	}
	return r, nil
}

// notePublic records that a run was allowed to show the brief to public
// providers. Once true it stays true: the status is a statement about what
// may already have left the machine.
func (p *Planner) notePublic(r *run, o Options) error {
	if o.AllowPublic {
		r.status.AllowPublic = true
		p.emit(events.KindWarning, "load", "", "--allow-public: public providers may be sent the whole brief in this run")
	}
	return r.saveStatus()
}

// resolveRepo turns the brief's repo into an absolute path to an existing
// directory. A URL is refused: cloning belongs to the git landing plan.
func resolveRepo(repo string) (string, error) {
	if strings.Contains(repo, "://") || strings.HasPrefix(repo, "git@") {
		return "", &brief.InvalidError{Reason: fmt.Sprintf("repo: %q is a URL; the planner needs an existing local path (cloning belongs to git landing)", repo)}
	}
	if repo == "~" || strings.HasPrefix(repo, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		repo = filepath.Join(home, strings.TrimPrefix(repo, "~"))
	}
	abs, err := filepath.Abs(repo)
	if err != nil {
		return "", err
	}
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return "", &brief.InvalidError{Reason: fmt.Sprintf("repo: %s is not an existing directory", abs)}
	}
	return abs, nil
}

// storeSecrets makes sure every secret the brief declares is in the vault
// under this run's scope. A value already stored for the run is left alone; a
// value stored for the harness (`gophermind brief vault set`) is copied; a
// person is asked when someone can be; otherwise the run does not start.
// Values never leave the vault and never appear in an error.
func (p *Planner) storeSecrets(b *brief.Brief) error {
	if len(b.Front.Secrets) == 0 {
		return nil
	}
	if p.d.OpenSecrets == nil {
		return errors.New("the brief declares secrets but no vault is available")
	}
	store, err := p.d.OpenSecrets()
	if err != nil {
		return err
	}
	scope := vault.RunScope(b.Front.ID)
	for _, s := range b.Front.Secrets {
		if _, ok := store.Get(scope, s.Name); ok {
			continue
		}
		v, ok := store.Get(vault.HarnessScope, s.Name)
		if !ok {
			if p.d.PromptSecret == nil {
				return fmt.Errorf("secret %s is not in the vault: run `gophermind brief vault set %s`, then start again", s.Name, s.Name)
			}
			v, err = p.d.PromptSecret(s.Name, s.Purpose)
			if err != nil {
				return fmt.Errorf("secret %s: %w", s.Name, err)
			}
			if v == "" {
				return fmt.Errorf("secret %s: an empty value was given", s.Name)
			}
		}
		if err := store.Set(scope, s.Name, v); err != nil {
			return fmt.Errorf("secret %s: %w", s.Name, err)
		}
	}
	return nil
}
