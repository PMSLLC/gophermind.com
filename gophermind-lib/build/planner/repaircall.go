package planner

import (
	"context"
	"go/parser"
	"go/token"
)

// repairSpec is cs with the stage suffix ":fix"; every other field is copied.
func repairSpec(cs callSpec) callSpec {
	cs.stage += ":fix"
	return cs
}

// renderRepairPrompt fills prompts/testwriter_fix.md with the file and the error text.
func renderRepairPrompt(src, errText string) (string, error) {
	return render("testwriter_fix", map[string]string{"File": src, "Errors": errText})
}

// repairTestFile asks the model once for a corrected file. unusable is empty
// when fixed is the file; otherwise it names why the reply is no use (the call
// failed, the reply held no Go file, or its package clause differs from src's)
// and fixed is "". err is non-nil only for a planner error (context cancelled,
// state write failure), never for a bad reply.
func (p *Planner) repairTestFile(ctx context.Context, r *run, cs callSpec, src, errText string) (fixed string, unusable string, err error) {
	prompt, err := renderRepairPrompt(src, errText)
	if err != nil {
		return "", "", err
	}
	var text string
	// The parse never fails, so the router never retries this call.
	parse := func(t string) error { text = t; return nil }
	if err := p.call(ctx, r, repairSpec(cs), prompt, parse); err != nil {
		if ctx.Err() != nil {
			return "", "", err
		}
		return "", "failed: " + err.Error(), nil
	}
	_, source := splitSourceReply(text)
	if source == "" {
		return "", "held no Go file", nil
	}
	want, ok1 := packageClauseName(src)
	got, ok2 := packageClauseName(source)
	if !ok1 || !ok2 || want != got {
		return "", "was for another package", nil
	}
	return source, "", nil
}

// packageClauseName returns the package name of a Go source, parsing only its package clause.
func packageClauseName(src string) (string, bool) {
	f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.PackageClauseOnly)
	if err != nil || f.Name == nil {
		return "", false
	}
	return f.Name.Name, true
}
