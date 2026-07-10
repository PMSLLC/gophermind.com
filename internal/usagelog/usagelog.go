// Package usagelog persists per-run token/cost usage to a JSONL file and
// summarizes spend by day and model, so users can see where budget goes.
package usagelog

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// Record is one run's usage.
type Record struct {
	Time             time.Time `json:"time"`
	Model            string    `json:"model"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	CostUSD          float64   `json:"cost_usd"`
}

// Append writes one record as a JSONL line (creating the file if needed).
func Append(path string, r Record) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

// Load reads all usage records from path (missing file = none).
func Load(path string) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var recs []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r Record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, fmt.Errorf("parse usage line: %w", err)
		}
		recs = append(recs, r)
	}
	return recs, sc.Err()
}

// agg accumulates totals for a group.
type agg struct {
	runs             int
	promptTokens     int
	completionTokens int
	cost             float64
}

// Report summarizes records grouped by day and by model, with an overall total.
func Report(recs []Record) string {
	if len(recs) == 0 {
		return "no usage recorded\n"
	}
	byDay := map[string]*agg{}
	byModel := map[string]*agg{}
	var total agg
	for _, r := range recs {
		day := r.Time.UTC().Format("2006-01-02")
		addAgg(byDay, day, r)
		addAgg(byModel, r.Model, r)
		total.runs++
		total.promptTokens += r.PromptTokens
		total.completionTokens += r.CompletionTokens
		total.cost += r.CostUSD
	}

	var b strings.Builder
	b.WriteString("Usage by day:\n")
	writeGroup(&b, byDay)
	b.WriteString("\nUsage by model:\n")
	writeGroup(&b, byModel)
	fmt.Fprintf(&b, "\nTotal: %d runs, %d prompt + %d completion tokens, $%.4f\n",
		total.runs, total.promptTokens, total.completionTokens, roundCost(total.cost))
	return b.String()
}

func addAgg(m map[string]*agg, key string, r Record) {
	a := m[key]
	if a == nil {
		a = &agg{}
		m[key] = a
	}
	a.runs++
	a.promptTokens += r.PromptTokens
	a.completionTokens += r.CompletionTokens
	a.cost += r.CostUSD
}

func writeGroup(b *strings.Builder, m map[string]*agg) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		a := m[k]
		fmt.Fprintf(b, "  %-14s %d runs  %d tok  $%.4f\n",
			k, a.runs, a.promptTokens+a.completionTokens, roundCost(a.cost))
	}
}

// roundCost trims floating error to 4 decimals for display, and prints 0.05
// rather than 0.0500000001.
func roundCost(c float64) float64 {
	return float64(int64(c*10000+0.5)) / 10000
}

// TotalCost sums the cost across records — used for cumulative budget alerts.
func TotalCost(recs []Record) float64 {
	var t float64
	for _, r := range recs {
		t += r.CostUSD
	}
	return roundCost(t)
}
