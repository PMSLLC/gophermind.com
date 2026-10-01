package executor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/gitland"
	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/proxy"
	"gophermind/gophermind-lib/briefv2/report"
	"gophermind/gophermind-lib/briefv2/settings"
)

// Options is everything one executor run talks to.
type Options struct {
	RunDir       string
	Repo         string
	Caller       Caller
	Board        blackboard.Blackboard
	Ledger       ledger.Ledger
	Gate         human.Gate  // nil means every escalation is a stop
	Sink         events.Sink // nil means events.Nop
	Settings     *settings.Config
	Secrets      Secrets      // nil when the brief declares none
	Git          gitland.Repo // nil means startRun makes gitland.NewCLI(Repo, runID) and closes it on exit
	Proxy        *proxy.Proxy // nil means the executor starts its own
	LedgerErrors func() int   // (*router.Router).LedgerErrors; nil means none
	Now          func() time.Time
}

// Report is the run report the executor returns.
type Report = report.Report

// validate checks the required fields and fills the defaults for a nil Sink
// and Now. It leaves Git alone: startRun builds the CLI once it knows the run
// id. The fresh-run dirty-tree check is not here; it belongs to startRun.
func (o *Options) validate() error {
	switch {
	case o.RunDir == "":
		return errors.New("executor: Options.RunDir is required")
	case o.Repo == "":
		return errors.New("executor: Options.Repo is required")
	case o.Caller == nil:
		return errors.New("executor: Options.Caller is required")
	case o.Board == nil:
		return errors.New("executor: Options.Board is required")
	case o.Ledger == nil:
		return errors.New("executor: Options.Ledger is required")
	case o.Settings == nil:
		return errors.New("executor: Options.Settings is required")
	case !filepath.IsAbs(o.RunDir):
		return errors.New("executor: Options.RunDir must be an absolute path")
	case !filepath.IsAbs(o.Repo):
		return errors.New("executor: Options.Repo must be an absolute path")
	}
	if fi, err := os.Stat(o.Repo); err != nil || !fi.IsDir() {
		return errors.New("executor: Options.Repo is not a directory")
	}
	if _, err := os.Lstat(filepath.Join(o.Repo, ".git")); err != nil {
		return errors.New("executor: Options.Repo is not the root of a git repository")
	}
	if err := o.Settings.Validate(); err != nil {
		return err
	}
	if o.Sink == nil {
		o.Sink = events.Nop
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return nil
}

// CheckWorkers is the check the CLI makes on --workers before it builds Options:
// leaves share one working tree, so only 1 is accepted. The wording matches
// settings.Validate's refusal of executor.workers.
func CheckWorkers(n int) error {
	if n != 1 {
		return errors.New("--workers must be 1 until leaf-isolated trees exist")
	}
	return nil
}

// String names the run folder, the repository and which collaborators are
// set. It never prints a collaborator's own value, so an Options handed to a
// log line, an error or %v cannot show a secret.
func (o Options) String() string {
	set := func(b bool) string {
		if b {
			return "set"
		}
		return "nil"
	}
	return fmt.Sprintf("executor.Options{RunDir: %q, Repo: %q, Secrets: %s, Gate: %s, Git: %s, Proxy: %s}",
		o.RunDir, o.Repo, set(o.Secrets != nil), set(o.Gate != nil), set(o.Git != nil), set(o.Proxy != nil))
}

// GoString is String, so %#v is as quiet as %v.
func (o Options) GoString() string { return o.String() }
