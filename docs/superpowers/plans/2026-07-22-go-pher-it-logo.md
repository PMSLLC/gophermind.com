# "Go Pher It" Logo Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give GopherMind a "Go Pher It" logo in three forms — a terminal ASCII banner, an SVG wordmark, and a raster/app-icon master — without renaming the product.

**Architecture:** The ASCII banner is a new exported constant in `internal/prompt` (sibling to the existing `GopherArt`), written into the startup splash by `internal/banner`. The SVG wordmark is a self-contained, hand-authored path file in `design/`, rasterized to PNG by a committed shell script that feeds the iOS app-icon set. Docs are updated last, once the artifacts exist.

**Tech Stack:** Go 1.25, `github.com/charmbracelet/lipgloss v1.1.1-0.20250404203927-76690c660834`, ImageMagick 7 (`magick`, present at `/opt/homebrew/bin/magick`), Chrome (visual verification), Xcode (`make ios-test`).

## Global Constraints

- Product name stays `GopherMind` everywhere: binary, Go module path, npm package, iOS bundle ID, repo name, docs prose. "Go Pher It" is tagline/logo art only. **No renames of any identifier, file, or package.**
- Palette, exact hex values: Ochre `#C98A45`, Teal `#5AA6BC`, Ink `#1C1917`, Paper `#FAF7F0`.
- ASCII banner: **≤ 46 columns on every line** (locks up under the existing ~46-column `GopherArt`, survives an 80-column terminal).
- SVG: **no `<text>` elements** — every glyph is path data. No external references, no embedded rasters, no network fetches. viewBox `0 0 512 320`.
- Do not modify the existing `GopherArt` gopher.
- Go raw string literals use backticks, so the banner constant must contain **no backtick characters** (backslashes are fine).
- Commit after every task.

---

### Task 1: `GoPherItBanner` constant

**Files:**
- Modify: `internal/prompt/art.go` (append after the `GopherArt` const, which ends at line 24)
- Create: `internal/prompt/art_test.go`
- Create: `internal/prompt/testdata/gopher_it_banner.golden`

**Interfaces:**
- Consumes: nothing.
- Produces: `prompt.GoPherItBanner` — an exported `string` constant, leading newline, three art lines, trailing newline. Consumed by Task 2.

- [ ] **Step 1: Write the failing test**

Create `internal/prompt/art_test.go`:

```go
package prompt

import (
	"path/filepath"
	"strings"
	"testing"

	"gophermind/internal/golden"
)

// TestGoPherItBannerWidth guards the lockup constraint: the tagline banner sits
// directly under GopherArt (~46 columns) and must survive an 80-column terminal.
func TestGoPherItBannerWidth(t *testing.T) {
	const maxCols = 46
	for i, line := range strings.Split(GoPherItBanner, "\n") {
		if n := len([]rune(line)); n > maxCols {
			t.Errorf("line %d is %d columns, want <= %d: %q", i, n, maxCols, line)
		}
	}
}

// TestGoPherItBannerIsASCII keeps the banner safe in a Go raw string literal and
// in terminals without wide-character support.
func TestGoPherItBannerIsASCII(t *testing.T) {
	if strings.Contains(GoPherItBanner, "\x60") {
		t.Error("banner contains a backtick; it cannot live in a raw string literal")
	}
	for _, r := range GoPherItBanner {
		if r > 126 || (r < 32 && r != '\n') {
			t.Errorf("non-printable-ASCII rune %q in banner", r)
		}
	}
}

// TestGoPherItBannerGolden snapshots the art so any accidental edit shows up in
// review. Update with: GOLDEN_UPDATE=1 go test ./internal/prompt/
func TestGoPherItBannerGolden(t *testing.T) {
	golden.Assert(t, filepath.Join("testdata", "gopher_it_banner.golden"), GoPherItBanner)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/prompt/ -run GoPherIt -v`
Expected: FAIL — `undefined: GoPherItBanner`

- [ ] **Step 3: Add the constant**

Append to `internal/prompt/art.go`:

```go
// GoPherItBanner is the "GO PHER IT" tagline wordmark shown directly under
// GopherArt at startup. Kept to 34 columns so it locks up flush under the ~46
// column gopher and survives an 80-column terminal. GopherMind remains the
// product name; this is tagline art only.
const GoPherItBanner = `
 __  __    _  | |  __  _     _ ___
/ _|/  \  |_) |_| |_  |_)    |  |
\__|\__/  |   | | |__ | \    |  |
`
```

- [ ] **Step 4: Create the golden file**

Run: `GOLDEN_UPDATE=1 go test ./internal/prompt/ -run GoPherItBannerGolden`
Then confirm the file exists and holds the art:

Run: `cat internal/prompt/testdata/gopher_it_banner.golden`
Expected: the three art lines, wrapped in blank lines.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/prompt/ -run GoPherIt -v`
Expected: PASS (all three tests)

- [ ] **Step 6: Commit**

```bash
git add internal/prompt/art.go internal/prompt/art_test.go internal/prompt/testdata/gopher_it_banner.golden
git commit -m 'feat(banner): add "GO PHER IT" ASCII tagline wordmark'
```

---

### Task 2: Wire the banner into the startup splash

**Files:**
- Modify: `internal/banner/banner.go:31` (inside `RenderWith`, between the `GopherArt` write and the version line)
- Create: `internal/banner/banner_tagline_test.go`

**Interfaces:**
- Consumes: `prompt.GoPherItBanner` (Task 1).
- Produces: no new exported API. `banner.Render()` and `banner.RenderWith(Options{...})` keep their existing signatures (`Render() string`, `RenderWith(o Options) string`) and now include the tagline.

- [ ] **Step 1: Write the failing test**

Create `internal/banner/banner_tagline_test.go`:

```go
package banner

import (
	"strings"
	"testing"
)

// TestRenderIncludesTagline verifies the "GO PHER IT" wordmark appears in the
// startup splash, under the gopher and above the version line.
func TestRenderIncludesTagline(t *testing.T) {
	out := RenderWith(Options{})

	// A distinctive slice of the tagline art: the "PH" of PHER.
	const needle = "|_) |_|"
	if !strings.Contains(out, needle) {
		t.Fatalf("tagline missing from banner; want substring %q in:\n%s", needle, out)
	}

	// The gopher's buck teeth must still come first, then the tagline.
	gopher := strings.Index(out, "|==|")
	tagline := strings.Index(out, needle)
	if gopher == -1 {
		t.Fatal("gopher art missing from banner")
	}
	if tagline < gopher {
		t.Errorf("tagline at %d precedes gopher at %d; want gopher first", tagline, gopher)
	}
}

// TestRenderTaglineIsUncolored keeps the plain-text path escape-free so the
// banner stays readable when piped or captured in tests.
func TestRenderTaglineIsUncolored(t *testing.T) {
	if strings.Contains(RenderWith(Options{}), "\x1b[") {
		t.Error("banner contains ANSI escapes; lipgloss should degrade to plain text off-TTY")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/banner/ -run Tagline -v`
Expected: FAIL — `tagline missing from banner`

- [ ] **Step 3: Write the tagline into `RenderWith`**

In `internal/banner/banner.go`, add the lipgloss import and the style, then write the tagline after the gopher.

Add to the import block:

```go
	"github.com/charmbracelet/lipgloss"
```

Add above `Render`:

```go
// taglineStyle tints the "GO PHER IT" wordmark with the teal from the gopher's
// glasses. lipgloss degrades to plain text when the output is not a color-capable
// TTY, so piped and captured output stays escape-free.
var taglineStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#5AA6BC"))
```

Replace `internal/banner/banner.go:31-32`:

```go
	b.WriteString(prompt.GopherArt)
	b.WriteString("\n")
```

with:

```go
	b.WriteString(prompt.GopherArt)
	b.WriteString(taglineStyle.Render(prompt.GoPherItBanner))
	b.WriteString("\n")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/banner/ -v`
Expected: PASS

- [ ] **Step 5: Verify the TUI banner tests still pass**

Run: `go test ./internal/tui/ -run Banner -v`
Expected: PASS — `TestBannerSurvivesFirstWindowSize` and `TestNoBannerSuppressesSplash`

- [ ] **Step 6: Look at the real thing**

The banner is only reachable through the TUI (`internal/tui/model.go:210` is the sole
call site), so print it directly with a throwaway test instead of launching the UI:

```bash
cat > internal/banner/print_scratch_test.go <<'EOF'
package banner

import "testing"

func TestPrintScratch(t *testing.T) { t.Log("\n" + RenderWith(Options{})) }
EOF
go test ./internal/banner/ -run TestPrintScratch -v
rm internal/banner/print_scratch_test.go
```

Expected: the gopher, then `GO PHER IT`, then the version line. Confirm no line
exceeds 80 columns and the tagline sits flush under the gopher, not offset.

Then confirm the scratch file is gone: `git status --porcelain internal/banner/`
Expected: only `banner.go` and `banner_tagline_test.go` listed.

- [ ] **Step 7: Full suite + commit**

```bash
go build ./... && go test ./...
git add internal/banner/banner.go internal/banner/banner_tagline_test.go
git commit -m 'feat(banner): show "GO PHER IT" tagline under the gopher at startup'
```

---

### Task 3: SVG wordmark (light + dark)

**Files:**
- Create: `design/gopher-it-logo.svg`
- Create: `design/gopher-it-logo-dark.svg`

**Interfaces:**
- Consumes: nothing.
- Produces: two self-contained SVG files at viewBox `0 0 512 320`, consumed by Task 4 (rasterizer) and Task 6 (README).

**Why paths, not `<text>`:** SVG `<text>` resolves against fonts on the *viewer's* machine. A `<text>` wordmark renders in Times on any machine lacking the font — unacceptable for a logo. Every glyph here is `<path>` data. Cost: the letterforms cannot be re-typed later by editing a string; changing the wordmark means redrawing paths. Task 6 records this in `design/README.md`.

**Exact layout (both variants share this geometry):**

| Element | Position | Size |
|---|---|---|
| Background rect | `0,0` | `512×320`, full bleed |
| Gopher mark | centered at `x=256`, top edge `y=24` | `128×128` |
| `GopherMind` wordmark | baseline `y=228`, centered on `x=256` | cap height 44px |
| `Go Pher It` tagline | baseline `y=282`, centered on `x=256` | cap height 22px |
| Teal underline | `y=296`, `x` from 176 to 336 | 4px tall, rounded caps |

**Color assignment:**

| Element | Light (`gopher-it-logo.svg`) | Dark (`gopher-it-logo-dark.svg`) |
|---|---|---|
| Background | `#FAF7F0` | `#1C1917` |
| Gopher fur | `#C98A45` | `#C98A45` |
| Gopher glasses | `#5AA6BC` | `#5AA6BC` |
| `Gopher` + `Mind` letters | `#1C1917` | `#FAF7F0` |
| `Pher` in the tagline | `#5AA6BC` | `#5AA6BC` |
| `Go` / `It` in the tagline | `#1C1917` | `#C98A45` |
| Underline | `#5AA6BC` | `#5AA6BC` |
| Glyph outlines | `#1C1917`, 2px | `#1C1917`, 2px |

**Hand-inked treatment:** each glyph is a filled path whose outline uses short, slightly non-collinear segments (offset control points by 0.5–1.5px from the true line) so edges read as brush-drawn rather than geometric. Do **not** use `feTurbulence` — it rasterizes differently across renderers and would make Task 4's PNGs inconsistent with the browser view.

- [ ] **Step 1: Draw the light variant**

Create `design/gopher-it-logo.svg`. Skeleton with the exact required scaffolding — fill in the glyph paths:

```xml
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 320" width="512" height="320" role="img" aria-label="GopherMind — Go Pher It">
  <title>GopherMind — Go Pher It</title>
  <rect x="0" y="0" width="512" height="320" fill="#FAF7F0"/>
  <g id="mark" transform="translate(192 24)">
    <!-- 128x128 gopher: ochre head, teal glasses, ink outlines, buck teeth -->
  </g>
  <g id="wordmark" fill="#1C1917" stroke="#1C1917" stroke-width="2" stroke-linejoin="round">
    <!-- GopherMind, cap height 44, baseline y=228, centered on x=256 -->
  </g>
  <g id="tagline" stroke="#1C1917" stroke-width="2" stroke-linejoin="round">
    <!-- Go (fill #1C1917) Pher (fill #5AA6BC) It (fill #1C1917), baseline y=282 -->
  </g>
  <path id="rule" d="M176 296 H336" stroke="#5AA6BC" stroke-width="4" stroke-linecap="round" fill="none"/>
</svg>
```

Worked example of the inked treatment — the `I` of `It` (cap height 22, baseline 282, stem at x=300), showing the deliberate 0.5–1.5px wobble on what would otherwise be straight edges:

```xml
<path d="M296.5 260.4 L305.2 260.0 L305.0 264.1 L302.3 264.4 L302.6 277.8 L305.4 278.1 L305.1 282.2 L296.4 281.9 L296.7 277.7 L299.4 277.5 L299.1 264.2 L296.3 264.5 Z" fill="#1C1917"/>
```

Every glyph follows this pattern: a closed path, coordinates nudged off the grid, no `<text>`, no external font.

- [ ] **Step 2: Verify it is self-contained and text-free**

```bash
grep -c "<text" design/gopher-it-logo.svg; grep -nE "href|url\(|@import|<image" design/gopher-it-logo.svg
```
Expected: `0` from the first command, no output from the second.

- [ ] **Step 3: Look at it in Chrome at full size**

Open `design/gopher-it-logo.svg` in Chrome at 1600px wide and screenshot it. Confirm: gopher reads as the same character as `design/gopher-genius-master-1024.png`, `Pher` is teal, the lockup is optically centered, no clipping at the viewBox edges.

- [ ] **Step 4: Verify legibility at favicon size**

Render at 32px and look at it:

```bash
magick -background none design/gopher-it-logo.svg -resize 32x32 /tmp/logo-32.png
```
Expected: a non-blank 32×32 PNG. View it. The tagline may go impressionistic at this size, but the gopher silhouette and the wordmark's overall shape must stay recognizable — not an undifferentiated smear. If it smears, thicken strokes and increase the tagline cap height, then re-check.

- [ ] **Step 5: Draw the dark variant**

Create `design/gopher-it-logo-dark.svg` — identical geometry and paths, with only the fills swapped per the color table above (background `#1C1917`, wordmark `#FAF7F0`, `Go`/`It` `#C98A45`).

- [ ] **Step 6: Verify the dark variant**

```bash
grep -c "<text" design/gopher-it-logo-dark.svg
```
Expected: `0`

Open it in Chrome and screenshot. Confirm the wordmark has real contrast against `#1C1917` and nothing disappeared into the background.

- [ ] **Step 7: Commit**

```bash
git add design/gopher-it-logo.svg design/gopher-it-logo-dark.svg
git commit -m 'feat(design): add "Go Pher It" SVG wordmark (light + dark)'
```

---

### Task 4: Reproducible rasterizer + PNG outputs

**Files:**
- Create: `scripts/render-logo.sh`
- Create: `design/gopher-it-1024.png`
- Create: `design/social-card-1280x640.png`

**Interfaces:**
- Consumes: `design/gopher-it-logo.svg`, `design/gopher-it-logo-dark.svg` (Task 3).
- Produces: `design/gopher-it-1024.png` (1024×1024, the new iOS app-icon master, consumed by Task 5) and `design/social-card-1280x640.png`.

Note on tooling: `rsvg-convert` is **not** installed locally; `magick` (ImageMagick 7) is, at `/opt/homebrew/bin/magick`, and its SVG delegate is `rsvg-convert`. So the script must try both and fail loudly rather than emit a blank or Times-fallback PNG.

- [ ] **Step 1: Write the script**

Create `scripts/render-logo.sh`:

```bash
#!/usr/bin/env bash
# Rasterize the "Go Pher It" SVG wordmark into the PNG artifacts in design/.
# Reproducible: re-run after any edit to design/gopher-it-logo*.svg.
set -euo pipefail

cd "$(dirname "$0")/.."

render() { # render <svg> <width> <height> <out>
  if command -v rsvg-convert >/dev/null 2>&1; then
    rsvg-convert -w "$2" -h "$3" -o "$4" "$1"
  elif command -v magick >/dev/null 2>&1; then
    magick -background none -density 384 "$1" -resize "$2x$3" "$4"
  else
    echo "error: need rsvg-convert or magick to rasterize SVG" >&2
    echo "       install with: brew install librsvg" >&2
    exit 1
  fi

  # A renderer that silently failed leaves a tiny or empty file. Catch it here
  # rather than shipping a blank icon.
  local bytes
  bytes=$(wc -c < "$4" | tr -d ' ')
  if [ "$bytes" -lt 2000 ]; then
    echo "error: $4 is only ${bytes} bytes — the SVG did not rasterize" >&2
    exit 1
  fi
  echo "rendered $4 (${bytes} bytes)"
}

render design/gopher-it-logo.svg 1024 1024 design/gopher-it-1024.png
render design/gopher-it-logo.svg 1280 640  design/social-card-1280x640.png
```

- [ ] **Step 2: Make it executable and run it**

```bash
chmod +x scripts/render-logo.sh
./scripts/render-logo.sh
```
Expected: two `rendered ...` lines, each well over 2000 bytes.

- [ ] **Step 3: Verify the outputs are real images**

```bash
magick identify design/gopher-it-1024.png design/social-card-1280x640.png
```
Expected: `... PNG 1024x1024 ...` and `... PNG 1280x640 ...`

Then **view both PNGs** and confirm they match what Chrome showed in Task 3 — same colors, no fallback typeface, no blank canvas. The 1280×640 card is a wider aspect than the 512×320 viewBox's 16:10; confirm the lockup is centered and not stretched. If it is stretched, add `-gravity center -extent 1280x640` to the card render and re-run.

- [ ] **Step 4: Verify the script fails loudly**

```bash
PATH=/usr/bin:/bin ./scripts/render-logo.sh; echo "exit=$?"
```
Expected: `error: need rsvg-convert or magick to rasterize SVG` and `exit=1` — not a silently-written blank PNG.

- [ ] **Step 5: Commit**

```bash
git add scripts/render-logo.sh design/gopher-it-1024.png design/social-card-1280x640.png
git commit -m 'build(design): add render-logo.sh and rasterized "Go Pher It" PNGs'
```

---

### Task 5: Regenerate the iOS app icon

**Files:**
- Modify: `ios/GopherMind/Assets.xcassets/AppIcon.appiconset/AppIcon-1024.png`

**Interfaces:**
- Consumes: `design/gopher-it-1024.png` (Task 4).
- Produces: no code API. `Contents.json` is unchanged — the asset catalog already references a single 1024×1024 `AppIcon-1024.png`.

- [ ] **Step 1: Record the current icon for comparison**

```bash
magick identify ios/GopherMind/Assets.xcassets/AppIcon.appiconset/AppIcon-1024.png
```
Expected: `... PNG 1024x1024 ...`. Note the dimensions so the replacement matches.

- [ ] **Step 2: Replace the icon**

```bash
cp design/gopher-it-1024.png ios/GopherMind/Assets.xcassets/AppIcon.appiconset/AppIcon-1024.png
magick identify ios/GopherMind/Assets.xcassets/AppIcon.appiconset/AppIcon-1024.png
```
Expected: `... PNG 1024x1024 ...`

- [ ] **Step 3: Verify the icon has no alpha channel**

iOS app icons must be fully opaque — a transparent icon is rejected at submission.

```bash
magick identify -format '%[channels]\n' ios/GopherMind/Assets.xcassets/AppIcon.appiconset/AppIcon-1024.png
```
Expected: `srgb` (not `srgba`). If it reports alpha, flatten it and re-check:

```bash
magick ios/GopherMind/Assets.xcassets/AppIcon.appiconset/AppIcon-1024.png \
  -background '#FAF7F0' -alpha remove -alpha off \
  ios/GopherMind/Assets.xcassets/AppIcon.appiconset/AppIcon-1024.png
```

- [ ] **Step 4: Confirm `Contents.json` still matches**

```bash
cat ios/GopherMind/Assets.xcassets/AppIcon.appiconset/Contents.json
```
Expected: it references `AppIcon-1024.png` at 1024×1024. Do not edit it.

- [ ] **Step 5: Build the iOS app**

Run: `make ios-test`
Expected: the app builds and the unit tests pass on a simulator. If the build fails for a reason unrelated to the icon (signing, simulator availability), report it rather than working around it.

- [ ] **Step 6: Commit**

```bash
git add ios/GopherMind/Assets.xcassets/AppIcon.appiconset/AppIcon-1024.png
git commit -m 'feat(ios): regenerate app icon from "Go Pher It" logo master'
```

---

### Task 6: Documentation wiring

**Files:**
- Modify: `README.md:1-37` (the centered ASCII block and the tagline line beneath it)
- Modify: `GOPHERLOGO.md` (whole file — currently documents art that does not ship)
- Modify: `design/README.md` (append palette + paths-not-`<text>` rationale)

**Interfaces:**
- Consumes: `design/gopher-it-logo.svg`, `design/gopher-it-logo-dark.svg` (Task 3).
- Produces: nothing consumed by later tasks. This is the final task.

- [ ] **Step 1: Replace the README header**

`README.md` opens with a `<div align="center">` wrapping a fenced ASCII gopher (lines 3–37), ending with the line `│              G O P H E R M I N D              │`. Replace that entire fenced block with the SVG, using a `<picture>` so it tracks the reader's GitHub theme:

```markdown
<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="design/gopher-it-logo-dark.svg">
  <img src="design/gopher-it-logo.svg" alt="GopherMind — Go Pher It" width="420">
</picture>
```

Leave everything from `**A tiny, hackable AI coding agent...**` onward untouched, including the badge block and the closing `</div>`.

- [ ] **Step 2: Verify the README renders**

```bash
grep -n "gopher-it-logo" README.md
ls -l design/gopher-it-logo.svg design/gopher-it-logo-dark.svg
```
Expected: both paths referenced in the README exist on disk, spelled exactly as committed. Relative paths must be `design/...` (repo-root-relative), since `README.md` lives at the root.

- [ ] **Step 3: Rewrite `GOPHERLOGO.md`**

The current file describes a 100×45 grayscale Midjourney→ASCII conversion. That art does **not** ship: `internal/prompt/art.go` holds a hand-drawn ~46-column gopher. Replace the whole file with:

```markdown
# GopherLogo.md

GopherMind's visual identity. The product name is **GopherMind**; **"Go Pher It"**
is the tagline and appears only as logo art.

## Palette

Sampled from `design/gopher-genius-master-1024.png`, the watercolor gopher that is
the master for every raster asset.

| Role | Hex |
|---|---|
| Ochre (fur) | `#C98A45` |
| Teal (glasses) | `#5AA6BC` |
| Ink | `#1C1917` |
| Paper | `#FAF7F0` |

## Terminal art

| Constant | File | Width | Shown by |
|---|---|---|---|
| `GopherArt` | `internal/prompt/art.go` | ~46 cols | `internal/banner.RenderWith` |
| `GoPherItBanner` | `internal/prompt/art.go` | 34 cols | `internal/banner.RenderWith`, tinted teal |

Both are hand-drawn ASCII, held under 46 columns so the lockup survives an
80-column terminal. `GoPherItBanner` is snapshot-tested against
`internal/prompt/testdata/gopher_it_banner.golden`; update it with
`GOLDEN_UPDATE=1 go test ./internal/prompt/`.

## Vector and raster assets

| File | What |
|---|---|
| `design/gopher-it-logo.svg` | Primary lockup, light |
| `design/gopher-it-logo-dark.svg` | Primary lockup, dark |
| `design/gopher-it-1024.png` | 1024×1024 master; source of the iOS app icon |
| `design/social-card-1280x640.png` | GitHub social preview |

Regenerate the PNGs after any SVG edit with `./scripts/render-logo.sh`.
```

- [ ] **Step 4: Append the rationale to `design/README.md`**

Add to the end of `design/README.md`:

```markdown
- `gopher-it-logo.svg` / `gopher-it-logo-dark.svg` — the "Go Pher It" lockup.

  **Every glyph is path data, not `<text>`.** SVG `<text>` resolves against fonts
  installed on the viewer's machine, so a `<text>` wordmark would fall back to
  Times anywhere the font is missing. Paths render identically everywhere and are
  what make the hand-inked look possible. The tradeoff: the letterforms cannot be
  re-typed by editing a string — changing the wordmark means redrawing paths.

  Palette: ochre `#C98A45` (fur), teal `#5AA6BC` (glasses, `Pher`), ink `#1C1917`,
  paper `#FAF7F0`. Regenerate PNGs with `./scripts/render-logo.sh`.
```

- [ ] **Step 5: Full verification sweep**

```bash
go build ./... && go test ./...
./scripts/render-logo.sh
grep -rn "Go Pher It" README.md GOPHERLOGO.md design/README.md
```
Expected: build and tests pass, renderer succeeds, tagline present in all three docs.

Confirm no accidental rename crept in:

```bash
grep -rn "gophermind" go.mod Makefile npm/package.json | head
```
Expected: module path, binary name, and npm package all still `gophermind`.

- [ ] **Step 6: Commit**

```bash
git add README.md GOPHERLOGO.md design/README.md
git commit -m 'docs: adopt "Go Pher It" logo in README and correct GOPHERLOGO.md'
```
