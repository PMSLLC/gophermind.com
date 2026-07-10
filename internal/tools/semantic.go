package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"gophermind/internal/embed"
	"gophermind/internal/safety"
)

// semanticSnippetMax caps how many characters of a matched chunk are shown.
const semanticSnippetMax = 300

// EmbedIndex returns a gated tool that builds a semantic index over the repo's
// files (chunked + embedded) and saves it to indexPath, so semantic_search can
// find relevant code without exhaustive grep. A nil provider means embeddings
// are unconfigured.
func EmbedIndex(root string, p embed.Provider, indexPath string) Tool {
	return Tool{
		Name:        "embed_index",
		Description: "Build a semantic (embeddings) index over the repository's files and save it, so semantic_search can retrieve relevant code by meaning. Specify file extensions to include.",
		Schema: object(map[string]any{
			"exts":        map[string]any{"type": "array", "description": "File extensions to index (e.g. [\".go\",\".md\"]); empty means all files.", "items": map[string]any{"type": "string"}},
			"incremental": map[string]any{"type": "boolean", "description": "Re-embed only files changed since the last index (via git), keeping the rest."},
		}),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if p == nil {
				return "", fmt.Errorf("embeddings are not configured; set GOPHERMIND_EMBED_URL and GOPHERMIND_EMBED_MODEL")
			}
			var a struct {
				Exts        []string `json:"exts"`
				Incremental bool     `json:"incremental"`
			}
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &a); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}

			// Incremental path: refresh only git-changed files against the existing
			// index. Falls back to a full build when no index exists yet.
			if a.Incremental {
				if existing, err := embed.LoadIndex(indexPath); err == nil {
					changed := gitChangedFiles(root)
					if len(changed) == 0 {
						return "Index is up to date (no changed files).", nil
					}
					idx, err := embed.UpdateIndex(ctx, p, root, a.Exts, existing, changed)
					if err != nil {
						return "", fmt.Errorf("update index: %w", err)
					}
					if err := idx.Save(indexPath); err != nil {
						return "", fmt.Errorf("save index: %w", err)
					}
					return fmt.Sprintf("Updated semantic index: %d changed file(s), %d chunks total.", len(changed), len(idx.Vectors)), nil
				}
			}

			idx, err := embed.BuildIndex(ctx, p, root, a.Exts)
			if err != nil {
				return "", fmt.Errorf("build index: %w", err)
			}
			if err := idx.Save(indexPath); err != nil {
				return "", fmt.Errorf("save index: %w", err)
			}
			return fmt.Sprintf("Built semantic index: %d chunks, saved to %s", len(idx.Vectors), indexPath), nil
		},
	}
}

// SemanticSearch returns a read-only tool that embeds a query and returns the
// most relevant indexed chunks (by cosine similarity). Requires a prior
// embed_index run (or an imported pack, when the pack argument is given).
func SemanticSearch(root string, p embed.Provider, indexPath, packsDir string) Tool {
	return Tool{
		Name:        "semantic_search",
		Description: "Search the semantic index (built by embed_index) for chunks most relevant to a query, returning file references and snippets. Pass 'pack' to search an imported knowledge pack instead. Read-only.",
		Schema: object(map[string]any{
			"query": str("The natural-language query to search for."),
			"k":     map[string]any{"type": "integer", "description": "How many results to return (default 5)."},
			"pack":  str("Optional name of an imported knowledge pack to search instead of the repo index."),
		}, "query"),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if p == nil {
				return "", fmt.Errorf("embeddings are not configured; set GOPHERMIND_EMBED_URL and GOPHERMIND_EMBED_MODEL")
			}
			var a struct {
				Query string `json:"query"`
				K     int    `json:"k"`
				Pack  string `json:"pack"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if strings.TrimSpace(a.Query) == "" {
				return "", fmt.Errorf("query is empty")
			}
			k := a.K
			if k <= 0 {
				k = 5
			}
			searchPath := indexPath
			if a.Pack != "" {
				pp, err := packPath(packsDir, a.Pack)
				if err != nil {
					return "", err
				}
				searchPath = pp
			}
			idx, err := embed.LoadIndex(searchPath)
			if err != nil {
				return "", fmt.Errorf("no index found (run embed_index or import_pack first): %w", err)
			}
			qv, err := p.Embed(ctx, []string{a.Query})
			if err != nil || len(qv) == 0 {
				return "", fmt.Errorf("embed query: %w", err)
			}
			hits := embed.TopK(qv[0], idx.Vectors, k)
			var b strings.Builder
			for _, h := range hits {
				snippet := h.Text
				if len(snippet) > semanticSnippetMax {
					snippet = snippet[:semanticSnippetMax] + "…"
				}
				fmt.Fprintf(&b, "── %s (score %.3f)\n%s\n\n", h.ID, h.Score, snippet)
			}
			if b.Len() == 0 {
				b.WriteString("(no results)\n")
			}
			return b.String(), nil
		},
	}
}

// packNameRe constrains knowledge-pack names so they stay inside the packs dir.
var packNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// packPath returns the on-disk path for a named knowledge pack.
func packPath(packsDir, name string) (string, error) {
	if !packNameRe.MatchString(name) {
		return "", fmt.Errorf("invalid pack name %q: use only letters, digits, '.', '_' and '-'", name)
	}
	return filepath.Join(packsDir, name+".json"), nil
}

// ImportPack returns a gated tool that indexes a folder of docs (markdown/text)
// into a named knowledge pack the model can later query via semantic_search.
func ImportPack(root string, p embed.Provider, packsDir string) Tool {
	return Tool{
		Name:        "import_pack",
		Description: "Index a folder of documents into a named knowledge pack (embeddings) that semantic_search can query with pack=<name>.",
		Schema: object(map[string]any{
			"name": str("Name for the knowledge pack."),
			"dir":  str("Folder (relative to the repo root) of documents to import."),
			"exts": map[string]any{"type": "array", "description": "File extensions to include (default [\".md\",\".txt\"]).", "items": map[string]any{"type": "string"}},
		}, "name", "dir"),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if p == nil {
				return "", fmt.Errorf("embeddings are not configured; set GOPHERMIND_EMBED_MODEL to import packs")
			}
			var a struct {
				Name string   `json:"name"`
				Dir  string   `json:"dir"`
				Exts []string `json:"exts"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			dst, err := packPath(packsDir, a.Name)
			if err != nil {
				return "", err
			}
			srcDir, err := safety.SafeJoin(root, a.Dir)
			if err != nil {
				return "", err
			}
			exts := a.Exts
			if len(exts) == 0 {
				exts = []string{".md", ".txt"}
			}
			idx, err := embed.BuildIndex(ctx, p, srcDir, exts)
			if err != nil {
				return "", fmt.Errorf("build pack: %w", err)
			}
			if err := idx.Save(dst); err != nil {
				return "", fmt.Errorf("save pack: %w", err)
			}
			return fmt.Sprintf("Imported knowledge pack %q: %d chunks. Query with semantic_search pack=%q.", a.Name, len(idx.Vectors), a.Name), nil
		},
	}
}

// RetrievalEval returns a read-only tool that scores retrieval quality (hit@k)
// of the semantic index against a JSONL fixtures file ({"query":..,"expect":..}
// per line), so chunking/embeddings can be tuned on data.
func RetrievalEval(root string, p embed.Provider, indexPath string) Tool {
	return Tool{
		Name:        "retrieval_eval",
		Description: "Score the semantic index's retrieval quality (hit@k) against a JSONL fixtures file with {\"query\":..,\"expect\":..} lines. Read-only.",
		Schema: object(map[string]any{
			"fixtures": str("Path to a JSONL fixtures file, relative to the repo root."),
			"k":        map[string]any{"type": "integer", "description": "Top-k to consider a hit (default 5)."},
		}, "fixtures"),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if p == nil {
				return "", fmt.Errorf("embeddings are not configured; set GOPHERMIND_EMBED_MODEL")
			}
			var a struct {
				Fixtures string `json:"fixtures"`
				K        int    `json:"k"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			k := a.K
			if k <= 0 {
				k = 5
			}
			full, err := safety.SafeJoin(root, a.Fixtures)
			if err != nil {
				return "", err
			}
			data, err := os.ReadFile(full)
			if err != nil {
				return "", fmt.Errorf("read fixtures: %w", err)
			}
			var cases []embed.EvalCase
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				var c embed.EvalCase
				if err := json.Unmarshal([]byte(line), &c); err != nil {
					return "", fmt.Errorf("parse fixture line: %w", err)
				}
				cases = append(cases, c)
			}
			idx, err := embed.LoadIndex(indexPath)
			if err != nil {
				return "", fmt.Errorf("no index found (run embed_index first): %w", err)
			}
			score, err := embed.HitAtK(ctx, p, idx, cases, k)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("hit@%d = %.2f%% over %d fixtures", k, score*100, len(cases)), nil
		},
	}
}

// gitChangedFiles returns repo-relative paths that git reports as modified or
// untracked (via `git status --porcelain`), used for incremental re-indexing.
// Returns nil when git is unavailable or the dir is not a repo.
func gitChangedFiles(root string) []string {
	cmd := exec.Command("git", "-C", root, "status", "--porcelain", "--untracked-files=all")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var files []string
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 4 {
			continue
		}
		// Porcelain format: "XY <path>" (or "XY <old> -> <new>" for renames).
		path := strings.TrimSpace(line[3:])
		if i := strings.Index(path, " -> "); i >= 0 {
			path = path[i+4:]
		}
		if path != "" {
			files = append(files, path)
		}
	}
	return files
}
