package plan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gophermind/gophermind-lib/lockfile"
	"gophermind/gophermind-lib/plantree"
)

// Completer runs one prompt and returns the model's reply. Every call must
// start from a fresh context: no history from any earlier call. AgentCompleter
// is the production implementation.
type Completer interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

// ErrBriefChanged is returned when a run is resumed against a brief or chunk
// size different from the one it started with. The cursor is only valid against
// identical chunk boundaries (same brief and same chunk size). Resuming against
// a different brief or chunk size would mix two plans.
var ErrBriefChanged = errors.New("plan: the brief or chunk size changed since this run started")

// Options tunes RunPass1. Zero values pick the defaults.
type Options struct {
	ProjectName string
	ChunkBytes  int // default DefaultChunkBytes
	OverviewCap int // default OverviewCapBytes
}

// Result summarizes one RunPass1 call.
type Result struct {
	Chunks    int     // chunks in the whole brief
	Processed int     // chunks processed by this call
	Created   Created // nodes added by this call
}

const (
	briefFile = "brief.md"
	stateFile = "pass1.json"
)

// pass1State is the resume cursor for a skeleton run. It is only a cursor:
// the tree and overview hold the real state, and a chunk that was merged but
// not yet recorded here is simply merged again, which changes nothing. The cursor
// is only valid against identical chunk boundaries (same brief and same chunk size).
type pass1State struct {
	BriefSHA256 string `json:"brief_sha256"`
	Chunks      int    `json:"chunks"`
	ChunkBytes  int    `json:"chunk_bytes"`
	Next        int    `json:"next"`
}

func statePath(repo *plantree.Repo) string {
	return filepath.Join(repo.Dir(), "_state", stateFile)
}

func loadState(repo *plantree.Repo) (pass1State, bool, error) {
	b, err := os.ReadFile(statePath(repo))
	if errors.Is(err, os.ErrNotExist) {
		return pass1State{}, false, nil
	}
	if err != nil {
		return pass1State{}, false, err
	}
	var s pass1State
	if err := json.Unmarshal(b, &s); err != nil {
		return pass1State{}, false, fmt.Errorf("plan: reading %s: %w", statePath(repo), err)
	}
	return s, true, nil
}

func saveState(repo *plantree.Repo, s pass1State) error {
	if err := os.MkdirAll(filepath.Dir(statePath(repo)), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return lockfile.WriteAtomic(statePath(repo), append(b, '\n'), 0o644)
}

// RunPass1 reads the brief in bounded chunks, one fresh-context pass per
// chunk, and builds the skeleton tree and running overview. It can be called
// again after any error and continues from the first unprocessed chunk.
func RunPass1(ctx context.Context, repo *plantree.Repo, brief string, c Completer, opt Options) (Result, error) {
	if opt.ProjectName == "" {
		opt.ProjectName = "project"
	}
	if opt.ChunkBytes < 1 {
		opt.ChunkBytes = DefaultChunkBytes
	}
	if opt.OverviewCap < 1 {
		opt.OverviewCap = OverviewCapBytes
	}
	chunks := SplitBrief(brief, opt.ChunkBytes)
	if len(chunks) == 0 {
		return Result{}, errors.New("plan: the brief is empty")
	}
	sum := sha256.Sum256([]byte(brief))
	digest := hex.EncodeToString(sum[:])

	if err := ensureRoot(repo, opt.ProjectName); err != nil {
		return Result{}, err
	}
	state, found, err := loadState(repo)
	if err != nil {
		return Result{}, err
	}
	if found && (state.BriefSHA256 != digest || state.Chunks != len(chunks) || state.ChunkBytes != opt.ChunkBytes) {
		return Result{}, ErrBriefChanged
	}
	if !found {
		state = pass1State{BriefSHA256: digest, Chunks: len(chunks), ChunkBytes: opt.ChunkBytes}
		if err := saveBrief(repo, brief); err != nil {
			return Result{}, err
		}
		if err := saveState(repo, state); err != nil {
			return Result{}, err
		}
	}

	res := Result{Chunks: len(chunks)}
	for i := state.Next; i < len(chunks); i++ {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		created, err := runChunk(ctx, repo, c, opt, chunks[i], len(chunks))
		if err != nil {
			return res, fmt.Errorf("plan: chunk %d of %d: %w", i+1, len(chunks), err)
		}
		res.Created.add(created)
		res.Processed++
		state.Next = i + 1
		if err := saveState(repo, state); err != nil {
			return res, err
		}
	}
	return res, nil
}

func ensureRoot(repo *plantree.Repo, name string) error {
	if _, err := repo.Get(plantree.RootID); err == nil {
		return nil
	} else if !errors.Is(err, plantree.ErrNotFound) {
		return err
	}
	return repo.Init(plantree.Node{
		SchemaVersion: plantree.SchemaVersion,
		ID:            plantree.RootID,
		Title:         name,
		NodeRevision:  1,
		ContextDigest: "The plan for " + name + ".",
		DependsOn:     []string{},
		Planning:      plantree.Planning{Stage: plantree.StageSkeleton},
	})
}

func saveBrief(repo *plantree.Repo, brief string) error {
	if err := os.MkdirAll(repo.Dir(), 0o755); err != nil {
		return err
	}
	return lockfile.WriteAtomic(filepath.Join(repo.Dir(), briefFile), []byte(brief), 0o644)
}

// runChunk performs one skeleton pass: build the prompt, ask, parse (once more
// with the rejection reason if the reply is malformed), merge, refresh the
// overview.
func runChunk(ctx context.Context, repo *plantree.Repo, c Completer, opt Options, chunk Chunk, total int) (Created, error) {
	overview, err := ReadOverview(repo.Dir())
	if err != nil {
		return Created{}, err
	}
	outline, err := Outline(repo)
	if err != nil {
		return Created{}, err
	}
	prompt := Pass1Prompt(opt.ProjectName, overview, outline, chunk, total)

	reply, err := c.Complete(ctx, prompt)
	if err != nil {
		return Created{}, err
	}
	out, perr := ParsePass1(reply)
	if perr != nil {
		reply, err = c.Complete(ctx, RetryPrompt(prompt, reply, perr.Error()))
		if err != nil {
			return Created{}, err
		}
		if out, err = ParsePass1(reply); err != nil {
			return Created{}, fmt.Errorf("the model's reply was rejected twice: %w", err)
		}
	}

	created, err := Merge(repo, out)
	if err != nil {
		return created, err
	}
	text := out.Overview
	if len(text) > opt.OverviewCap {
		if shorter, cerr := c.Complete(ctx, CompressPrompt(text, opt.OverviewCap)); cerr == nil && shorter != "" {
			text = shorter
		}
		text = FitOverview(text, opt.OverviewCap)
	}
	if err := WriteOverview(repo.Dir(), text); err != nil {
		return created, err
	}
	return created, nil
}
