package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"gophermind/internal/freellm"
)

// runFree implements "gophermind free". It writes to out and returns a process
// exit code, so it is testable without spawning a subprocess.
func runFree(args []string, out io.Writer) int {
	if len(args) == 0 {
		return freeUsageText(out)
	}
	switch args[0] {
	case "list":
		if len(args) > 1 && args[1] == "--json" {
			return freeListJSON(out)
		}
		return freeList(out)
	case "show":
		if len(args) < 2 {
			fmt.Fprintln(out, "usage: gophermind free show <profile>")
			return 2
		}
		return freeShow(out, args[1])
	case "check":
		if len(args) < 2 {
			fmt.Fprintln(out, "usage: gophermind free check <profile>")
			return 2
		}
		return freeCheck(out, args[1])
	case "usage":
		return freeUsage(out)
	default:
		fmt.Fprintf(out, "unknown subcommand %q\n", args[0])
		return freeUsageText(out)
	}
}

func freeUsageText(out io.Writer) int {
	fmt.Fprintln(out, "usage: gophermind free <list|show|check|usage>")
	fmt.Fprintln(out, "  list           every free provider, no-key ones first")
	fmt.Fprintln(out, "  show <profile> full details and the exact env to set")
	fmt.Fprintln(out, "  check <profile> probe the endpoint's model list")
	fmt.Fprintln(out, "  usage          the free-usage odometer and trip meters")
	return 2
}

// termsFlags renders the free-tier obligations for a table cell.
func termsFlags(t freellm.Terms) string {
	return strings.Join(freellm.TermsFlags(t), ", ")
}

func freeList(out io.Writer) int {
	r := freellm.Load()
	fmt.Fprintf(out, "Free LLM providers (registry vendored %s from github.com/mnfst/awesome-free-llm-apis)\n\n", r.LastUpdated())
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PROFILE\tPROVIDER\tKEY\tDEFAULT MODEL\tTERMS\tLINK")
	for _, c := range freellm.Compats() {
		key := "required"
		if c.NoKey {
			key = "no key"
		}
		model := c.DefaultModel
		if !c.Supported {
			key, model = "-", "not runnable"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", c.Profile, c.Upstream, key, model, termsFlags(c.Terms), c.Website)
	}
	tw.Flush()
	fmt.Fprintln(out, "\nRun one with:  gophermind --profile <profile> ask \"hello\"")
	fmt.Fprintln(out, "Details with:  gophermind free show <profile>")
	return 0
}

// freeListEntry is the machine-readable form of one Compats() row. It backs
// the hidden "gophermind free list --json" mode, which exists so
// scripts/gen-free-providers-doc.sh can generate docs/free-providers.md
// straight from compat.go instead of hand-typing provider facts into the
// doc, which is exactly what let free-llm7 drift into the doc after it was
// marked unsupported.
type freeListEntry struct {
	Profile      string   `json:"profile"`
	Upstream     string   `json:"upstream"`
	Website      string   `json:"website"`
	NoKey        bool     `json:"no_key"`
	Supported    bool     `json:"supported"`
	DefaultModel string   `json:"default_model,omitempty"`
	Terms        []string `json:"terms,omitempty"`
	Note         string   `json:"note,omitempty"`
}

func freeListJSON(out io.Writer) int {
	entries := make([]freeListEntry, 0, len(freellm.Compats()))
	for _, c := range freellm.Compats() {
		entries = append(entries, freeListEntry{
			Profile:      c.Profile,
			Upstream:     c.Upstream,
			Website:      c.Website,
			NoKey:        c.NoKey,
			Supported:    c.Supported,
			DefaultModel: c.DefaultModel,
			Terms:        freellm.TermsFlags(c.Terms),
			Note:         c.Note,
		})
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(entries); err != nil {
		fmt.Fprintf(out, "error encoding JSON: %v\n", err)
		return 1
	}
	return 0
}

// freeShowLinkLine renders the "Link:" line of "free show" for an
// attribution. It reads only a.Link()/a.IsReferral() (never a Compat field
// directly), so it can never surface an affiliate URL without
// freellm.ReferralMarker, and it honors GOPHERMIND_NO_AFFILIATE because that
// opt-out is already applied inside a by AttributionFor. It returns "" (no
// line at all) whenever the link is not a referral, matching the prior
// behavior of only showing this line for an affiliate URL.
func freeShowLinkLine(a freellm.Attribution) string {
	if !a.IsReferral() {
		return ""
	}
	return fmt.Sprintf("Link:     %s %s\n", a.Link(), freellm.ReferralMarker)
}

func freeShow(out io.Writer, profile string) int {
	c, ok := freellm.CompatFor(profile)
	if !ok {
		fmt.Fprintf(out, "unknown free profile %q; run: gophermind free list\n", profile)
		return 1
	}
	p, hasUpstream := freellm.Load().Lookup(c.Upstream)

	fmt.Fprintf(out, "%s  (%s)\n", c.Upstream, c.Profile)
	fmt.Fprintf(out, "Website:  %s\n", c.Website)
	if hasUpstream {
		fmt.Fprintf(out, "Signup:   %s\n", p.URL)
		fmt.Fprintf(out, "About:    %s\n", p.Description)
	}
	if !c.Supported {
		fmt.Fprintf(out, "\nNot runnable as a profile: %s\n", c.Note)
		return 0
	}
	fmt.Fprintf(out, "Endpoint: %s\n", c.BaseURL)
	fmt.Fprintf(out, "Default:  %s\n", c.DefaultModel)
	if f := termsFlags(c.Terms); f != "" {
		fmt.Fprintf(out, "Terms:    %s\n", f)
	}
	if c.Note != "" {
		fmt.Fprintf(out, "Note:     %s\n", c.Note)
	}
	if a, ok := freellm.AttributionFor(c.Profile, c.DefaultModel); ok {
		fmt.Fprint(out, freeShowLinkLine(a))
	}

	if hasUpstream {
		fmt.Fprintln(out, "\nModels:")
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "  ID\tCONTEXT\tMAX OUT\tMODALITY\tRATE LIMIT")
		for _, m := range p.Models {
			if m.ID == "" {
				continue
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", m.ID, m.Context, m.MaxOutput, m.Modality, m.RateLimit)
		}
		tw.Flush()
	}

	fmt.Fprintln(out, "\nTo use it:")
	if !c.NoKey {
		fmt.Fprintf(out, "  export %s='<your key>'\n", apiKeyEnvFor(c.Profile))
	}
	fmt.Fprintf(out, "  gophermind --profile %s ask \"hello\"\n", c.Profile)
	return 0
}

// apiKeyEnvFor mirrors config.profileEnvKey: the per-profile env var prefix.
func apiKeyEnvFor(profile string) string {
	up := strings.ToUpper(profile)
	up = strings.ReplaceAll(up, "-", "_")
	return "GOPHERMIND_PROFILE_" + up + "_API_KEY"
}

func freeCheck(out io.Writer, profile string) int {
	c, ok := freellm.CompatFor(profile)
	if !ok {
		fmt.Fprintf(out, "unknown free profile %q; run: gophermind free list\n", profile)
		return 1
	}
	if !c.Supported {
		fmt.Fprintf(out, "%s is not runnable as a profile: %s\n", c.Profile, c.Note)
		return 1
	}
	key := os.Getenv(apiKeyEnvFor(c.Profile))
	if key == "" && !c.NoKey {
		fmt.Fprintf(out, "%s needs a key. Set %s, then run this again.\n", c.Profile, apiKeyEnvFor(c.Profile))
		return 1
	}
	fmt.Fprintf(out, "Probing %s ...\n", c.BaseURL)
	ids, err := freellm.Check(context.Background(), c.BaseURL, key, nil)
	if err != nil {
		fmt.Fprintf(out, "FAILED: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "OK: %d models advertised\n", len(ids))
	for i, id := range ids {
		if i >= 20 {
			fmt.Fprintf(out, "  ... and %d more\n", len(ids)-20)
			break
		}
		fmt.Fprintf(out, "  %s\n", id)
	}
	var found bool
	for _, id := range ids {
		if id == c.DefaultModel {
			found = true
			break
		}
	}
	if !found && len(ids) > 0 {
		fmt.Fprintf(out, "\nWarning: the default model %q is not in this list.\n", c.DefaultModel)
	}
	return 0
}

func freeUsage(out io.Writer) int {
	path := freellm.OdometerPath()
	o, err := freellm.LoadOdometer(path)
	if err != nil {
		fmt.Fprintf(out, "could not read the odometer at %s: %v\n", path, err)
		return 1
	}
	tokens, requests := o.Reading()
	fmt.Fprintln(out, "Free usage odometer")
	fmt.Fprintf(out, "  %s tokens over %s requests\n", freellm.Commas(tokens), freellm.Commas(requests))
	if !o.Since.IsZero() {
		fmt.Fprintf(out, "  since %s\n", o.Since.Format("2006-01-02"))
	}

	if len(o.Per) > 0 {
		fmt.Fprintln(out, "\nBy provider:")
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "  PROFILE\tTOKENS\tREQUESTS\tLAST USED")
		for _, c := range freellm.Compats() {
			t, ok := o.Per[c.Profile]
			if !ok {
				continue
			}
			last := "-"
			if !t.LastSeen.IsZero() {
				last = t.LastSeen.Format("2006-01-02 15:04")
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", c.Profile, freellm.Commas(t.Tokens), freellm.Commas(t.Requests), last)
		}
		tw.Flush()

		fmt.Fprintln(out, "\nCurrent quota windows:")
		now := time.Now()
		for _, c := range freellm.Compats() {
			if _, used := o.Per[c.Profile]; !used || !c.Supported {
				continue
			}
			var parts []string
			for _, m := range freellm.TripMeters(o, c, now) {
				s := m.String()
				if m.Warn() {
					s += "  <- near the limit"
				}
				parts = append(parts, s)
			}
			fmt.Fprintf(out, "  %-20s %s\n", c.Profile, strings.Join(parts, "   "))
		}
	}
	return 0
}
