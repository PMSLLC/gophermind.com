package plan

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"gophermind/gophermind-lib/plantree"
)

// outlineCapBytes bounds the list of existing phases and tasks in a prompt.
const outlineCapBytes = 4000

// Outline lists the phases and tasks already in the tree, titles only, so a
// pass can attach to them instead of repeating them. It stops at
// outlineCapBytes and says how many entries it left out.
func Outline(repo *plantree.Repo) (string, error) {
	var b strings.Builder
	omitted := 0
	err := repo.Walk(func(n plantree.Node) error {
		var line string
		switch n.Kind() {
		case plantree.KindPhase:
			line = "- Phase: " + oneLine(n.Title) + "\n"
		case plantree.KindTask:
			line = "  - Task: " + oneLine(n.Title) + "\n"
		default:
			return nil
		}
		if b.Len()+len(line) > outlineCapBytes {
			omitted++
			return nil
		}
		b.WriteString(line)
		return nil
	})
	if err != nil {
		return "", err
	}
	if omitted > 0 {
		fmt.Fprintf(&b, "  (%d more phases and tasks not shown)\n", omitted)
	}
	return b.String(), nil
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none yet)"
	}
	return strings.TrimRight(s, "\n")
}

// Pass1Prompt builds the prompt for one skeleton pass. It carries the running
// overview, the outline of what already exists, and exactly one chunk of the
// brief, and nothing from any earlier conversation.
func Pass1Prompt(project, overview, outline string, c Chunk, total int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are breaking a project brief into a plan for %q. You see ONE part of the brief (part %d of %d), not all of it.\n\n", project, c.Index+1, total)
	b.WriteString("Running overview of the whole brief so far:\n")
	b.WriteString(orNone(overview))
	b.WriteString("\n\nPhases and tasks already in the plan. To add to one, reuse its title exactly. Never repeat an existing item:\n")
	b.WriteString(orNone(outline))
	title := ""
	if strings.TrimSpace(c.Title) != "" {
		title = fmt.Sprintf(" (section: %s)", oneLine(c.Title))
	}
	fmt.Fprintf(&b, "\n\nThis part of the brief%s:\n<<<BRIEF PART\n%s\nBRIEF PART>>>\n\n", title, strings.TrimRight(c.Text, "\n"))
	b.WriteString("Rules:\n")
	b.WriteString("- A phase groups related work. A task is a unit of work one agent can own. A step is the smallest independently verifiable piece, roughly one file change or one command with a check.\n")
	b.WriteString("- Every phase, task and step needs a digest: one or two sentences saying why it exists relative to its parent, understandable without reading the parent.\n")
	b.WriteString("- Add only what this part of the brief supports. Do not invent scope. If this part adds nothing new, return an empty phases list.\n")
	b.WriteString("- Every task needs an objective and at least one step, even if this part of the brief only outlines it.\n")
	fmt.Fprintf(&b, "- Rewrite the overview so it covers the whole brief so far, in under %d characters. Keep decisions, constraints and non-goals; drop detail that the plan itself now holds.\n", OverviewCapBytes)
	b.WriteString("- Ask a question only when something in this part of the brief is genuinely ambiguous and the answer changes the plan. Never ask what the brief already answers. At most 5 questions, each with 2 to 8 options (or none for a free-text question), \"recommended\" (option labels) if you have a recommendation, and \"affects\" naming the phase or task titles the answer changes. If nothing is ambiguous, return an empty questions array.\n")
	b.WriteString("- Do not call tools. Reply with ONE JSON object and nothing else, in this shape:\n")
	b.WriteString(`{"phases":[{"title":"","digest":"","objective":"","tasks":[{"title":"","digest":"","objective":"","steps":[{"title":"","digest":""}]}]}],"overview":"","questions":[{"question":"","why":"","options":[{"label":"","description":""}],"multi_select":false,"recommended":[],"rationale":"","affects":[]}]}`)
	b.WriteString("\n")
	return b.String()
}

// Bounds on the text a retry prompt adds to the original prompt.
const (
	retryReplyExcerptBytes = 1500
	retryProblemBytes      = 600
)

// cutBytes shortens s to at most max bytes at a rune boundary, adding "..." if
// it cut anything.
func cutBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

// RetryPrompt asks the model to correct a reply that was rejected. It quotes
// the start of the rejected reply so the model can see what it got wrong; the
// text it adds to original is bounded whatever the reply's size.
func RetryPrompt(original, reply, problem string) string {
	return original + "\n\nYour previous reply was rejected: " + cutBytes(problem, retryProblemBytes) +
		"\nYour previous reply began:\n" + cutBytes(reply, retryReplyExcerptBytes) +
		"\nReply again with ONE JSON object only, fixing that problem."
}

// CompressPrompt asks the model to shorten an overview that grew past its cap.
func CompressPrompt(overview string, cap int) string {
	return fmt.Sprintf("Compress this project overview to under %d characters. Keep decisions, constraints, non-goals and the shape of the plan; drop detail. Reply with the compressed overview text only.\n\n%s\n", cap, strings.TrimRight(overview, "\n"))
}
