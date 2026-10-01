package planner

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/settings"
)

// Caller is the one router method the planner uses. *router.Router satisfies it.
type Caller interface {
	CallParsed(ctx context.Context, info router.CallInfo, req provider.Request, parse func(text string) error) (router.Result, error)
}

// Secrets is the part of the vault the Load stage needs. *vault.Vault satisfies it.
type Secrets interface {
	Get(scope, name string) (string, bool)
	Set(scope, name, value string) error
}

// Deps is everything the planner talks to. Nothing here is the terminal: the
// run service and the desktop app pass their own gate and sink.
type Deps struct {
	Caller   Caller
	Gate     human.Gate
	Sink     events.Sink           // nil means events.Nop
	Board    blackboard.Blackboard // nil skips the blackboard rows (tests of early stages)
	Settings *settings.Config      // nil means settings.Default()

	// OpenSecrets is called only when the brief declares a secret.
	OpenSecrets func() (Secrets, error)
	// PromptSecret asks a person for one secret value; nil when nothing can
	// prompt (the file gate, tests).
	PromptSecret func(name, purpose string) (string, error)
	// LedgerErrors reports how many ledger writes failed so far
	// ((*router.Router).LedgerErrors); nil means none are counted.
	LedgerErrors func() int
	// ResetRun deletes the stored rows (blackboard, events, ledger) of a run id.
	// A new plan calls it once its run folder exists, so a reused id starts
	// clean. Nil means there is nothing to clear.
	ResetRun func(ctx context.Context, runID string) error
	Now      func() time.Time // nil means time.Now
}

// Options says what one Run call does.
type Options struct {
	BriefPath   string // plan: the brief file
	RunID       string // resume: the run to continue (BriefPath empty)
	Yes         bool   // --yes: approval is recorded as approved_by "flag"
	AllowPublic bool   // recorded in the run's status; the router option is set by the caller
	// StopAfter ends the run once the named stage has finished (load, clarify,
	// contract, decompose, coverage, approve). The run stays resumable.
	StopAfter string
}

type Outcome string

const (
	Done    Outcome = "done"    // every requested stage finished
	Waiting Outcome = "waiting" // a person has to answer before the run can go on
)

type Planner struct {
	d    Deps
	dump dumpState
}

func New(d Deps) *Planner {
	if d.Sink == nil {
		d.Sink = events.Nop
	}
	if d.Settings == nil {
		d.Settings = settings.Default()
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Planner{d: d}
}

// run is one brief being planned.
type run struct {
	id     string
	dir    string // the run folder
	repo   string // the target repository root, absolute
	src    []byte // brief.md as loaded
	brief  *brief.Brief
	reqs   []Requirement
	opts   Options
	status runStatus
}

func (r *run) path(name string) string { return filepath.Join(r.dir, filepath.FromSlash(name)) }

func (r *run) saveStatus() error { return writeJSON(r.path(stateStatus), r.status) }

// stage is one step of the pipeline. done reports whether its output already
// exists, which is what makes a resume skip it.
type stage struct {
	name string
	run  func(p *Planner, ctx context.Context, r *run) error
	done func(r *run) bool
}

// stages after Load, in order.
var stages = []stage{
	{"clarify", (*Planner).clarify, clarifyDone},
	{"contract", (*Planner).contract, contractDone},
	{"decompose", (*Planner).decompose, decomposeDone},
	{"coverage", (*Planner).coverage, coverageDone},
	{"approve", (*Planner).approve, approveDone},
	{"testwriter", (*Planner).testwriter, testwriterDone},
}

// Run plans a brief, or continues a run, until every stage is done, a person
// has to answer (Waiting), or something fails.
func (p *Planner) Run(ctx context.Context, o Options) (Outcome, error) {
	r, err := p.load(ctx, o)
	if err != nil {
		return "", err
	}
	if o.StopAfter == "load" {
		return Done, nil
	}
	defer p.noteLedgerErrors(r)
	for _, st := range stages {
		if st.done(r) {
			if o.StopAfter == st.name {
				return Done, nil
			}
			continue
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		p.emit(events.KindStageStarted, st.name, "", "")
		err := st.run(p, ctx, r)
		if errors.Is(err, human.ErrWaiting) {
			r.status.Waiting = st.name
			if serr := r.saveStatus(); serr != nil {
				return "", serr
			}
			p.emit(events.KindWaitingOnHuman, st.name, "", "waiting for an answer; run `gophermind brief resume "+r.id+"` once it is given")
			return Waiting, nil
		}
		if err != nil {
			return "", fmt.Errorf("planner: %s: %w", st.name, err)
		}
		if r.status.Waiting != "" {
			r.status.Waiting = ""
			if err := r.saveStatus(); err != nil {
				return "", err
			}
		}
		p.emit(events.KindStageFinished, st.name, "", "")
		if o.StopAfter == st.name {
			return Done, nil
		}
	}
	return Done, nil
}

func (p *Planner) emit(kind, stageName, nodeID, msg string) {
	p.d.Sink.Emit(events.Event{Kind: kind, Stage: stageName, NodeID: nodeID, Message: msg, At: p.d.Now().UTC()})
}

func (p *Planner) noteLedgerErrors(r *run) {
	if p.d.LedgerErrors == nil {
		return
	}
	if n := p.d.LedgerErrors(); n != r.status.LedgerErrors {
		r.status.LedgerErrors = n
		_ = r.saveStatus()
	}
}

// callSpec says what one model call carries, for the router and the ledger.
type callSpec struct {
	stage     string // full stage name, for example decompose:greeting
	taskType  string // clarify, contract, decompose, coverage, testwrite
	nodeID    string
	nodeClass string // leaf calls only (spec: node_class is for a leaf call); every stage call leaves it empty
	scope     router.Scope
	maxTokens int
	// maxGrown, when positive, is the cap on the doubled budget the router
	// retries with after a truncated reply; zero keeps the router's default.
	maxGrown int
}

// call makes one model call through the router. parse receives the raw reply
// text; an error from it marks the reply malformed and takes the retry path.
func (p *Planner) call(ctx context.Context, r *run, cs callSpec, prompt string, parse func(text string) error) error {
	info := router.CallInfo{RunID: r.id, Stage: cs.stage, NodeID: cs.nodeID, Tier: router.TierStrong, Scope: cs.scope,
		TaskType: cs.taskType, NodeClass: cs.nodeClass}
	req := request(cs.stage, prompt, cs.maxTokens)
	req.MaxGrownTokens = cs.maxGrown
	if p.dumpDir(r) != "" {
		inner := parse
		parse = func(text string) error {
			p.dumpReply(r, cs.stage, req, text)
			return inner(text)
		}
	}
	_, err := p.d.Caller.CallParsed(ctx, info, req, parse)
	var ce *router.ChainExhausted
	if errors.As(err, &ce) && ce.OnlyPrivacy() {
		return fmt.Errorf("%w; this call carries %s scope and every model in the %s tier is a public provider: "+
			"add a private provider to that tier in gophermind.yaml, or start the run with --allow-public", err, cs.scope, ce.Tier)
	}
	return err
}

const maxQuestionsPerCall = 3

// pendingQuestion is _state/question.json: a question a model asked that no
// person has answered yet. A resume asks the person again without asking the
// model again.
type pendingQuestion struct {
	Stage    string `json:"stage"`
	Question string `json:"question"`
}

// callAsking is call plus the pause-and-ask protocol: a reply that is a
// QUESTION: goes to the human gate (or, when the brief says assume and
// document, back to the model with that instruction) and the call is made
// again with the answer added.
func (p *Planner) callAsking(ctx context.Context, r *run, cs callSpec, prompt string, parse func(text string) error) error {
	as, err := loadAnswers(r)
	if err != nil {
		return err
	}
	for _, a := range as.Answers {
		if a.Stage == cs.stage {
			prompt += answerNote(a.Question, a.Answer)
		}
	}
	for asked := 0; ; asked++ {
		var pend pendingQuestion
		found, err := readJSON(r.path(stateQuestion), &pend)
		if err != nil {
			return err
		}
		question := ""
		if found && pend.Stage == cs.stage {
			question = pend.Question
		} else {
			err := p.call(ctx, r, cs, prompt, func(text string) error {
				if q, ok := Question(text); ok {
					question = q
					return nil
				}
				question = ""
				return parse(text)
			})
			if err != nil {
				return err
			}
			if question == "" {
				return nil
			}
		}
		if asked >= maxQuestionsPerCall {
			return fmt.Errorf("the model asked more than %d questions in stage %s (the last was %d bytes)", maxQuestionsPerCall, cs.stage, len(question))
		}
		if r.brief.Front.OnAmbiguity == "assume_and_document" {
			prompt += "\n\nNo human is available. Choose the most conservative option and record it in the node's \"assumptions\" array."
			continue
		}
		if err := writeJSON(r.path(stateQuestion), pendingQuestion{Stage: cs.stage, Question: question}); err != nil {
			return err
		}
		id := fmt.Sprintf("%s-q%d", strings.ReplaceAll(cs.stage, ":", "-"), len(as.Answers)+1)
		got, err := p.ask(ctx, []human.Question{{ID: id, Text: question}})
		if err != nil {
			return err
		}
		if len(got) != 1 {
			return fmt.Errorf("the human gate returned %d answers for 1 question", len(got))
		}
		as.Answers = append(as.Answers, answer{ID: id, Stage: cs.stage, Question: question, Answer: got[0].Text, Assumed: got[0].Assumed})
		if err := writeJSON(r.path(fileAnswers), as); err != nil {
			return err
		}
		if err := removeFile(r.path(stateQuestion)); err != nil {
			return err
		}
		prompt += answerNote(question, got[0].Text)
	}
}

// ask puts questions to the human gate.
func (p *Planner) ask(ctx context.Context, qs []human.Question) ([]human.Answer, error) {
	if p.d.Gate == nil {
		return nil, errors.New("a question needs an answer and no human gate is configured")
	}
	return p.d.Gate.Ask(ctx, qs)
}

func answerNote(question, text string) string {
	return "\n\nAnswer from the owner to your question:\n" + question + "\n" + text
}
