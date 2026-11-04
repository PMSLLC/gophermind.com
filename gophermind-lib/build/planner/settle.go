package planner

import (
	"context"
	"errors"
	"fmt"
	"go/format"
	"path"
	"strings"

	"gophermind/gophermind-lib/build/contract"
	"gophermind/gophermind-lib/build/events"
)

// settleInput is what both callers have after the reply's JSON part is parsed.
type settleInput struct {
	c        *contract.Contracts
	id       string    // node id
	file     string    // contract file of the function, e.g. check/all.go
	rel      string    // repo-relative test file path
	funcName string    // TestAll
	sig      string    // the function's signature, for internalTarget
	pkg      string    // the contract package, for a repaired file's normalization
	pkgs     importSet // the module's packages, for a repaired file's import check
	others   []string  // other accepted test sources of the package
	stubs    []string  // stubSources(c, path.Dir(file))
}

// settled is a test file that passed every check, and what was done to it.
type settled struct {
	src      string
	renamed  bool
	dropped  []string
	fixes    []mechanicalFix
	repaired bool
	comments int // long comment blocks removed from a repaired file
}

// settleTestFile runs every check after the JSON parse: exercises, host rule,
// shared names, unused imports, format, compile, mechanical fixes, one repair
// call, compile. A nil error means src compiled; errNoGo comes back with the
// settled file when there is no go tool, for the caller to warn about once.
// Any other error is the refusal for the model's own file: a compile refusal
// names the lines of the file before the mechanical fixes.
func (p *Planner) settleTestFile(ctx context.Context, r *run, cs callSpec, in settleInput, src string) (settled, error) {
	s, fixedSrc, refusal, err := p.settleChecks(cs, in, src)
	if err != nil || refusal == nil {
		return s, err
	}
	return p.repairAndSettle(ctx, r, cs, in, fixedSrc, refusal)
}

// settleSyntax gives a file refused as not valid Go its repair call. The
// repaired file is normalized and settled like a fresh one, with no second
// repair. A reply with no usable file is refused with the original error; a
// repaired file that still fails is refused with it prefixed.
func (p *Planner) settleSyntax(ctx context.Context, r *run, cs callSpec, in settleInput, se *syntaxRefusal) (settled, error) {
	return p.repairAndSettle(ctx, r, cs, in, se.Src, se)
}

// repairAndSettle makes the one repair call for src, refused with orig, and
// settles the reply with no further repair. It emits test_file_repaired on
// success and test_file_repair_unusable for a reply with no usable file.
func (p *Planner) repairAndSettle(ctx context.Context, r *run, cs callSpec, in settleInput, src string, orig error) (settled, error) {
	fixed, ok, err := p.repairTestFile(ctx, r, cs, src, orig.Error())
	if err != nil {
		return settled{}, err
	}
	if !ok {
		p.emit(events.KindWarning, cs.stage, in.id, "test_file_repair_unusable: the repair reply for node "+in.id+" held no Go file")
		return settled{}, orig
	}
	_, _, why := internalTarget(in.sig)
	norm, comments, err := normalizeTestFile(fixed, in.pkg, in.funcName, in.c.Module, why, in.pkgs)
	if err != nil {
		return settled{}, fmt.Errorf("after a repair call: %w", orig)
	}
	s, _, refusal, err := p.settleChecks(cs, in, norm)
	if (err != nil && !errors.Is(err, errNoGo)) || refusal != nil {
		return settled{}, fmt.Errorf("after a repair call: %w", orig)
	}
	s.repaired, s.comments = true, comments
	if err == nil {
		p.emit(events.KindWarning, cs.stage, in.id, "test_file_repaired: the test file of node "+in.id+" compiled after a repair call")
	}
	return s, err
}

// settleChecks runs the checks of settleTestFile up to the repair call. err is
// a refusal that no repair call is made for, or errNoGo (s is then the
// formatted file). refusal is the compile refusal of the file before the
// mechanical fixes, and fixedSrc the file after them, for the repair call.
// Both nil: s compiled.
func (p *Planner) settleChecks(cs callSpec, in settleInput, src string) (s settled, fixedSrc string, refusal, err error) {
	name, _, _ := internalTarget(in.sig)
	if err := checkTestExercises(src, name); err != nil {
		return settled{}, "", nil, err
	}
	if reachesNetwork(in.c, in.id) || importsNet(src) {
		if err := checkNoRealHost(src); err != nil {
			return settled{}, "", nil, err
		}
	}
	dir := path.Dir(in.rel)
	src, renamed, err := resolveSharedNames(src, strings.TrimPrefix(in.funcName, "Test"), in.others, in.stubs)
	if err == errDeclaredName {
		err = richDeclaredName(src, in.c, dir, err)
	}
	if err != nil {
		return settled{}, "", nil, err
	}
	src, dropped := dropUnusedImports(src, moduleImportNames(in.c))
	formatted, ferr := format.Source([]byte(src))
	if ferr != nil {
		return settled{}, "", nil, fmt.Errorf("test file cannot be formatted: %v", ferr)
	}
	src = string(formatted)
	s = settled{src: src, renamed: renamed, dropped: dropped}
	diags, err := compileCheckDiags(in.c, in.rel, src, in.others)
	if err != nil || len(diags) == 0 {
		return s, "", nil, err
	}
	fixedSrc, fixes, unfixed := applyMechanicalFixes(src, diags)
	for _, f := range fixes {
		p.emit(events.KindWarning, cs.stage, in.id, "test_file_mechanical_fix: "+f.Kind+" "+f.Name+" in the test file of node "+in.id)
	}
	if len(fixes) > 0 {
		again, err := compileCheckDiags(in.c, in.rel, fixedSrc, in.others)
		if err != nil || len(again) == 0 {
			s.src, s.fixes = fixedSrc, fixes
			return s, "", nil, err
		}
		if len(unfixed) == 0 {
			// The fixed file fails on lines the first compile did not
			// report; only the fixed file's own lines can name them.
			return settled{}, fixedSrc, refuseCompileDiags(in.c, dir, fixedSrc, again), nil
		}
	}
	return settled{}, fixedSrc, refuseCompileDiags(in.c, dir, src, unfixed), nil
}
