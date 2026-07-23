# "Go Pher It" logo — design

Date: 2026-07-22
Status: approved (pending spec review)

## Goal

Give GopherMind a visual logo built around the tagline **"Go Pher It"**, in three
forms: a terminal ASCII banner, an SVG wordmark asset, and a raster/app-icon
master. The product name does not change.

## Naming constraint

`GopherMind` remains the product name everywhere: binary, Go module path, npm
package, iOS bundle ID, repo name, and all docs prose. **"Go Pher It" is a
tagline and logo-art element only.** No renames, no identifier churn.

## Brand palette

Sampled from `design/gopher-genius-master-1024.png` (the Midjourney watercolor
gopher that is already the iOS icon master):

| Role | Hex | Use |
|---|---|---|
| Ochre (fur) | `#C98A45` | warm accent, dark-mode tagline |
| Teal (glasses) | `#5AA6BC` | `Pher` accent, tagline underline |
| Ink | `#1C1917` | outlines, light-mode type |
| Paper | `#FAF7F0` | light background, dark-mode type |

## Deliverable 1 — SVG wordmark

Files: `design/gopher-it-logo.svg`, `design/gopher-it-logo-dark.svg`

Lockup, top to bottom: gopher mark → `GopherMind` wordmark → `Go Pher It`
tagline. Hand-inked style — irregular contours and visible ink outlines so the
type reads as drawn by the same hand as the watercolor art. `Pher` and the
tagline carry teal `#5AA6BC`; the rest is ink on paper. The dark variant is
paper-on-ink with the tagline in ochre `#C98A45`.

**Every glyph is authored as SVG path data, not `<text>`.** SVG `<text>` resolves
against fonts installed on the viewer's machine, so a `<text>`-based wordmark
would render differently (or fall back to Times) on any machine lacking the font.
Paths render identically everywhere and are what makes the inked look possible.
Cost: the letterforms are drawn once and cannot be re-typed by editing a string —
changing the wordmark means redrawing paths. `design/README.md` records this.

Canvas: 512×320 viewBox, no external references, no embedded rasters — the SVG
must be self-contained and open correctly in a browser with no network.

## Deliverable 2 — ASCII banner

The live art is `GopherArt` in `internal/prompt/art.go`: a hand-drawn gopher
about 46 columns wide. (`GOPHERLOGO.md` describes a 100-column grayscale
conversion that is *not* what ships; that doc is stale and gets corrected as part
of this work.)

Add a sibling constant `GoPherItBanner` in `internal/prompt/art.go`: block-letter
`GO PHER IT` in the same character vocabulary as the existing art.

Hard constraint: **≤ 46 columns wide**, so it locks up flush under the gopher and
survives an 80-column terminal (the existing TUI banner tests run at width 80).
This means ~4-column glyphs with tight inter-letter spacing and two-column word
gaps; if `GO PHER IT` cannot be made legible within 46 columns on one line, it
wraps to two lines rather than exceeding the width.

Wiring: `internal/banner/RenderWith` writes `GoPherItBanner` immediately after
`prompt.GopherArt`, before the version line. Colored with lipgloss (teal) when
the terminal is color-capable, plain text otherwise — the constant itself stays
free of escape codes so tests can assert on it directly.

## Deliverable 3 — Raster + app icon

- `design/gopher-it-1024.png` — the lockup composited over the master art,
  1024×1024, the new iOS app-icon master.
- `design/social-card-1280x640.png` — GitHub social preview.
- `scripts/render-logo.sh` — committed, reproducible SVG→PNG rendering. Tries
  `rsvg-convert`, falls back to `magick` (ImageMagick 7 is present locally and
  delegates SVG to rsvg-convert when available). Exits non-zero with a clear
  message if neither renderer can rasterize SVG, rather than emitting a blank or
  Times-fallback PNG.
- Regenerate `ios/GopherMind/Assets.xcassets/AppIcon.appiconset/` from the new
  1024 master.

## Deliverable 4 — Wiring

- `README.md` header uses `design/gopher-it-logo.svg`.
- `GOPHERLOGO.md` corrected: document the actual shipped 46-column art and the
  new `GoPherItBanner`, and drop the stale 100-column conversion description.
- `design/README.md` notes the palette and the paths-not-`<text>` decision.

## Out of scope

Website, npm package art, Homebrew formula description, and any change to the
existing `GopherArt` gopher itself.

## Verification

1. `go build ./... && go test ./...` passes.
2. New test in `internal/prompt`: `GoPherItBanner` is ≤46 columns on every line
   and contains the letters of `GO PHER IT` in order (a byte-stable golden file,
   matching the existing golden-test pattern in that package).
3. Existing `internal/tui/banner_test.go` still passes — the gopher needle
   `|==|` remains present and the `--no-banner` path still suppresses everything.
4. Both SVGs open in Chrome and screenshot cleanly at 1600px and at 32px
   (favicon size) — the tagline must remain legible, not mush, at 32px.
5. `scripts/render-logo.sh` runs clean and the PNGs are non-blank (checked by
   file size and by viewing them).
6. `make ios-test` still builds after the icon set is regenerated.
