package executor

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gophermind/gophermind-lib/briefv2/packer"
)

// guidanceFrom turns the build groups of a function node into the guidance the
// implement prompt carries. It reads construction, security, observability,
// performance and portability and nothing else: rationale, alternatives,
// refactor notes, assumptions, open questions, decision ids and the embedded
// decisions carry reasoning from the brief, and an implement call may be shown
// to a public provider. A group answered "not applicable" gives no lines; a
// node planned before the groups existed gives no guidance. A group that is
// present but cannot be read is an error, never a silent omission: leaving out
// the security group would let an implement call run without its warnings.
func guidanceFrom(d leafDoc) ([]packer.Guidance, error) {
	var out []packer.Guidance
	add := func(section string, lines []string) {
		if len(lines) > 0 {
			out = append(out, packer.Guidance{Section: section, Lines: lines})
		}
	}
	if c := d.Construction; c != nil {
		var lines []string
		if strings.TrimSpace(c.ApproachChosen) != "" {
			lines = append(lines, "Approach: "+c.ApproachChosen)
		}
		for i, s := range c.Steps {
			lines = append(lines, fmt.Sprintf("Step %d: %s", i+1, s))
		}
		add("construction", lines)
	}
	var sec struct {
		NotApplicable   string   `json:"not_applicable"`
		TrustBoundary   string   `json:"trust_boundary"`
		UntrustedInputs []string `json:"untrusted_inputs"`
		Threats         []struct {
			Threat     string `json:"threat"`
			Mitigation string `json:"mitigation"`
		} `json:"threats"`
	}
	if ok, err := decodeGroup(d.Security, &sec); err != nil {
		return nil, fmt.Errorf("the security group is not readable: %w", err)
	} else if ok && sec.NotApplicable == "" {
		lines := []string{"Trust boundary: " + sec.TrustBoundary}
		for _, in := range sec.UntrustedInputs {
			lines = append(lines, "Untrusted input: "+in)
		}
		for _, t := range sec.Threats {
			lines = append(lines, fmt.Sprintf("Threat: %s. Mitigation: %s.", t.Threat, t.Mitigation))
		}
		add("security", lines)
	}
	var obs struct {
		NotApplicable string `json:"not_applicable"`
		LogEvents     []struct {
			Level  string   `json:"level"`
			Msg    string   `json:"msg"`
			Fields []string `json:"fields"`
		} `json:"log_events"`
		Metrics []struct {
			Name string `json:"name"`
			Kind string `json:"kind"`
		} `json:"metrics"`
	}
	if ok, err := decodeGroup(d.Observability, &obs); err != nil {
		return nil, fmt.Errorf("the observability group is not readable: %w", err)
	} else if ok && obs.NotApplicable == "" {
		var lines []string
		for _, e := range obs.LogEvents {
			l := fmt.Sprintf("Log at %s: %q", e.Level, e.Msg)
			if len(e.Fields) > 0 {
				l += " with fields " + strings.Join(e.Fields, ", ")
			}
			lines = append(lines, l)
		}
		for _, m := range obs.Metrics {
			lines = append(lines, fmt.Sprintf("Metric: %s (%s)", m.Name, m.Kind))
		}
		add("observability", lines)
	}
	var perf struct {
		NotApplicable string `json:"not_applicable"`
		Complexity    string `json:"complexity"`
		MaxLatencyMS  *int   `json:"max_latency_ms"`
		AllocBudget   string `json:"alloc_budget"`
		Concurrency   string `json:"concurrency"`
		HotPath       bool   `json:"hot_path"`
	}
	if ok, err := decodeGroup(d.Performance, &perf); err != nil {
		return nil, fmt.Errorf("the performance group is not readable: %w", err)
	} else if ok && perf.NotApplicable == "" {
		lines := []string{"Complexity: " + perf.Complexity}
		if perf.MaxLatencyMS != nil {
			lines = append(lines, fmt.Sprintf("Latency budget: %d ms", *perf.MaxLatencyMS))
		}
		if perf.AllocBudget != "" {
			lines = append(lines, "Allocation budget: "+perf.AllocBudget)
		}
		lines = append(lines, "Concurrency: "+perf.Concurrency)
		if perf.HotPath {
			lines = append(lines, "Hot path: yes")
		}
		add("performance", lines)
	}
	var port struct {
		NotApplicable string   `json:"not_applicable"`
		OS            []string `json:"os"`
		Arch          []string `json:"arch"`
		GoMin         string   `json:"go_min"`
		CGO           bool     `json:"cgo"`
		BuildTags     []string `json:"build_tags"`
		Deps          []string `json:"deps"`
	}
	if ok, err := decodeGroup(d.Portability, &port); err != nil {
		return nil, fmt.Errorf("the portability group is not readable: %w", err)
	} else if ok && port.NotApplicable == "" {
		var lines []string
		if port.GoMin != "" {
			lines = append(lines, "Go "+port.GoMin+" or newer")
		}
		if len(port.OS) > 0 {
			lines = append(lines, "Operating systems: "+strings.Join(port.OS, ", "))
		}
		if len(port.Arch) > 0 {
			lines = append(lines, "Architectures: "+strings.Join(port.Arch, ", "))
		}
		if port.CGO {
			lines = append(lines, "cgo: allowed")
		} else {
			lines = append(lines, "cgo: not allowed")
		}
		if len(port.BuildTags) > 0 {
			lines = append(lines, "Build tags: "+strings.Join(port.BuildTags, ", "))
		}
		if len(port.Deps) > 0 {
			lines = append(lines, "Dependencies: "+strings.Join(port.Deps, ", "))
		}
		add("portability", lines)
	}
	return out, nil
}

// decodeGroup decodes a raw group into v. It reports whether the group exists;
// a group that exists but does not decode is an error.
func decodeGroup(raw json.RawMessage, v any) (bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return false, nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return false, errors.New("its JSON does not match the group")
	}
	return true, nil
}
