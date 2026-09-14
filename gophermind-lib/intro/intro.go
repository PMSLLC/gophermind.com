// Package intro plays a short, dependency-free animated gopher mascot before the
// interactive TUI starts. It renders a truecolor ANSI gopher that fades in,
// opens its eyes with a reflection sweep, and settles — about 1.5 seconds, and
// skippable by any keypress. The animation is a no-op unless it is running on an
// interactive, truecolor terminal that is at least 80x30, so it never disrupts
// piped, dumb, or small terminals.
//
// The mascot art and motion are adapted from gophermind's standalone
// gopher-animation prototype; only the sequencing (shortened) and the terminal
// plumbing (skip + gating) are new.
package intro

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/term"
)

// ANSI control sequences. The intro uses the alternate screen buffer so it never
// pollutes the user's scrollback, and restores the main screen on exit.
const (
	esc        = "\x1b["
	clear      = esc + "2J"
	home       = esc + "H"
	hideCursor = esc + "?25l"
	showCursor = esc + "?25h"
	altScreen  = esc + "?1049h"
	mainScreen = esc + "?1049l"
	reset      = esc + "0m"
)

// RGB is a 24-bit truecolor value.
type RGB struct{ R, G, B int }

// Palette is the mascot's full color set; dimPalette scales it for the fade-in.
type Palette struct {
	Fur, FurDark, FurLight RGB
	Cyan, CyanBright       RGB
	White, Pupil, Nose     RGB
	Tooth, Text, Dim       RGB
}

// Segment is a colored run of text within a rendered line.
type Segment struct {
	Text  string
	Color RGB
	Bold  bool
}

// AnimState is the per-frame animation state consumed by mascotLines.
type AnimState struct {
	Tick       int
	EyeX       int // -1 look left, 0 center, +1 look right
	Blink      bool
	Shine      int
	ToothPhase bool
	EarPhase   int
	Bounce     int
	Breath     int
}

var basePalette = Palette{
	Fur:        RGB{202, 132, 55},
	FurDark:    RGB{112, 67, 28},
	FurLight:   RGB{238, 176, 92},
	Cyan:       RGB{44, 177, 201},
	CyanBright: RGB{157, 245, 255},
	White:      RGB{238, 248, 248},
	Pupil:      RGB{13, 19, 22},
	Nose:       RGB{86, 55, 40},
	Tooth:      RGB{242, 239, 211},
	Text:       RGB{125, 224, 238},
	Dim:        RGB{90, 106, 112},
}

func fg(c RGB) string { return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", c.R, c.G, c.B) }

func bold(on bool) string {
	if on {
		return esc + "1m"
	}
	return esc + "22m"
}

func scale(c RGB, k float64) RGB {
	clamp := func(v int) int {
		if v < 0 {
			return 0
		}
		if v > 255 {
			return 255
		}
		return v
	}
	return RGB{clamp(int(float64(c.R) * k)), clamp(int(float64(c.G) * k)), clamp(int(float64(c.B) * k))}
}

// dimPalette scales every color toward black by k (0..1) for the fade-in, but
// keeps the pupils from going fully black so the eyes stay legible.
func dimPalette(p Palette, k float64) Palette {
	pk := k
	if pk < 0.25 {
		pk = 0.25
	}
	p.Fur = scale(p.Fur, k)
	p.FurDark = scale(p.FurDark, k)
	p.FurLight = scale(p.FurLight, k)
	p.Cyan = scale(p.Cyan, k)
	p.CyanBright = scale(p.CyanBright, k)
	p.White = scale(p.White, k)
	p.Pupil = scale(p.Pupil, pk)
	p.Nose = scale(p.Nose, k)
	p.Tooth = scale(p.Tooth, k)
	p.Text = scale(p.Text, k)
	p.Dim = scale(p.Dim, k)
	return p
}

func seg(text string, c RGB) Segment  { return Segment{Text: text, Color: c} }
func bseg(text string, c RGB) Segment { return Segment{Text: text, Color: c, Bold: true} }

func visibleWidth(line []Segment) int {
	n := 0
	for _, s := range line {
		n += utf8.RuneCountInString(s.Text)
	}
	return n
}

// renderLine centers a line of segments within width and emits its ANSI colors.
func renderLine(line []Segment, width int) string {
	pad := (width - visibleWidth(line)) / 2
	if pad < 0 {
		pad = 0
	}
	var out strings.Builder
	out.WriteString(strings.Repeat(" ", pad))
	for _, s := range line {
		out.WriteString(fg(s.Color))
		out.WriteString(bold(s.Bold))
		out.WriteString(s.Text)
	}
	out.WriteString(reset)
	return out.String()
}

// eyeInterior renders one 13-column lens, placing the pupil per EyeX and, when a
// shine is sweeping across this lens, a bright reflection slash.
func eyeInterior(st AnimState, p Palette, lens int) []Segment {
	if st.Blink {
		return []Segment{seg("   ───────   ", p.FurDark)}
	}

	pupilPos := 6 + st.EyeX*2
	chars := []rune("             ")
	chars[pupilPos-1] = '◉'
	chars[pupilPos] = '●'
	chars[pupilPos+1] = '◉'

	localShine := st.Shine - lens*16
	if localShine >= 0 && localShine < len(chars) {
		chars[localShine] = '╱'
	}

	out := make([]Segment, 0, len(chars))
	var buf strings.Builder
	current := p.White
	flush := func() {
		if buf.Len() > 0 {
			out = append(out, seg(buf.String(), current))
			buf.Reset()
		}
	}
	for _, r := range chars {
		c := p.White
		if r == '●' || r == '◉' {
			c = p.Pupil
		}
		if r == '╱' {
			c = p.CyanBright
		}
		if c != current {
			flush()
			current = c
		}
		buf.WriteRune(r)
	}
	flush()
	return out
}

// mascotLines builds the full gopher for one frame.
func mascotLines(st AnimState, p Palette) [][]Segment {
	leftEye := eyeInterior(st, p, 0)
	rightEye := eyeInterior(st, p, 1)

	earL, earR := "╭≋╮", "╭≋╮"
	if st.EarPhase == 1 {
		earL = "╭≈╮"
	}
	if st.EarPhase == 2 {
		earR = "╭≈╮"
	}

	bodyPad := ""
	if st.Breath > 0 {
		bodyPad = " "
	}

	toothLead := ""
	if st.ToothPhase {
		toothLead = " "
	}
	toothTop := toothLead + "╭────────╮╭────────╮"
	toothMid1 := toothLead + "│  ▒▒▒▒  ││  ▒▒▒▒  │"
	toothMid2 := toothLead + "│  ▒▒▒▒  ││  ▒▒▒▒  │"
	toothBottom := toothLead + "╰────────╯╰────────╯"

	lines := [][]Segment{
		{seg("        ", p.Fur), bseg(earL, p.FurDark), seg("        ╱╲        ", p.FurLight), bseg(earR, p.FurDark)},
		{seg("     ╭──", p.FurDark), seg("╯  ╰", p.Fur), seg("──────╯  ╰", p.FurDark), seg("──╮", p.FurDark)},
		{seg("   ╭─╯", p.FurDark), seg("  ≋≋≋≋≋≋≋≋≋≋≋≋≋≋≋  ", p.Fur), seg("╰─╮", p.FurDark)},
		{seg("  ╱", p.FurDark), bseg("╭───────────────╮", p.Cyan), seg("╭───╮", p.Cyan), bseg("╭───────────────╮", p.Cyan), seg("╲", p.FurDark)},
	}

	row := []Segment{seg(" ╱ ", p.FurDark), bseg("│", p.Cyan), seg(" ", p.White)}
	row = append(row, leftEye...)
	row = append(row, seg(" ", p.White), bseg("│", p.Cyan), bseg("═══", p.Cyan), bseg("│", p.Cyan), seg(" ", p.White))
	row = append(row, rightEye...)
	row = append(row, seg(" ", p.White), bseg("│", p.Cyan), seg(" ╲", p.FurDark))
	lines = append(lines, row)

	row2 := []Segment{seg("│  ", p.FurDark), bseg("│", p.Cyan), seg(" ", p.White)}
	row2 = append(row2, leftEye...)
	row2 = append(row2, seg(" ", p.White), bseg("│", p.Cyan), seg("   ", p.Fur), bseg("│", p.Cyan), seg(" ", p.White))
	row2 = append(row2, rightEye...)
	row2 = append(row2, seg(" ", p.White), bseg("│", p.Cyan), seg("  │", p.FurDark))
	lines = append(lines, row2)

	lines = append(lines,
		[]Segment{seg("│  ", p.FurDark), bseg("╰───────────────╯", p.Cyan), seg("╲_╱", p.FurDark), bseg("╰───────────────╯", p.Cyan), seg("  │", p.FurDark)},
		[]Segment{seg("│      ", p.FurDark), seg("≋≋≋≋≋", p.Fur), bseg("╭───╮", p.Nose), seg("≋≋≋≋≋", p.Fur), seg("      │", p.FurDark)},
		[]Segment{seg("│     ", p.FurDark), seg("≋≋≋≋", p.FurLight), bseg("╰─●─╯", p.Nose), seg("≋≋≋≋", p.FurLight), seg("     │", p.FurDark)},
		[]Segment{seg("│       ", p.FurDark), seg("╲  ╲___/  ╱", p.FurDark), seg("       │", p.FurDark)},
		[]Segment{seg("│      ", p.FurDark), bseg(toothTop, p.Tooth), seg("      │", p.FurDark)},
		[]Segment{seg("│      ", p.FurDark), bseg(toothMid1, p.Tooth), seg("      │", p.FurDark)},
		[]Segment{seg("│      ", p.FurDark), bseg(toothMid2, p.Tooth), seg("      │", p.FurDark)},
		[]Segment{seg("│      ", p.FurDark), bseg(toothBottom, p.Tooth), seg("      │", p.FurDark)},
		[]Segment{seg("│     ", p.FurDark), seg("╭─────────────────╮", p.FurLight), seg("     │", p.FurDark)},
		[]Segment{seg("│   ", p.FurDark), seg(bodyPad+"╱ ≋≋≋≋≋≋≋≋≋≋≋≋≋≋≋ ╲"+bodyPad, p.Fur), seg("   │", p.FurDark)},
		[]Segment{seg("│  ", p.FurDark), seg(bodyPad+"│ ≋≋≋≋≋≋≋≋≋≋≋≋≋≋≋≋≋ │"+bodyPad, p.Fur), seg("  │", p.FurDark)},
		[]Segment{seg("│  ", p.FurDark), seg(bodyPad+"│ ≋≋≋≋≋≋≋≋≋≋≋≋≋≋≋≋≋ │"+bodyPad, p.Fur), seg("  │", p.FurDark)},
		[]Segment{seg("│   ", p.FurDark), seg(bodyPad+"╲ ≋≋≋≋≋≋≋≋≋≋≋≋≋≋≋ ╱"+bodyPad, p.Fur), seg("   │", p.FurDark)},
		[]Segment{seg(" ╲     ", p.FurDark), seg("╰───────────────╯", p.FurDark), seg("     ╱", p.FurDark)},
		[]Segment{seg("  ╰──────", p.FurDark), seg("╮       ╭", p.FurDark), seg("──────╯", p.FurDark)},
		[]Segment{seg("          ", p.Fur), seg("╰───────╯", p.FurDark)},
	)
	return lines
}

// frame renders one complete animation frame, positioned from the top-left.
func frame(st AnimState, p Palette, width int) string {
	var out strings.Builder
	out.WriteString(home)
	for _, line := range mascotLines(st, p) {
		out.WriteString(renderLine(line, width))
		out.WriteString("\x1b[K\n") // clear to EOL so shorter frames leave no residue
	}
	return out.String()
}

// playSequence writes the short intro (fade-in → eyes open + reflection sweep →
// settle with one blink) to out, polling skip between frames. It returns true if
// skip fired (the animation was cut short). The whole sequence is ~1.5s.
func playSequence(out io.Writer, width int, skip func() bool) bool {
	step := func(st AnimState, p Palette, d time.Duration) bool {
		io.WriteString(out, frame(st, p, width))
		time.Sleep(d)
		return skip != nil && skip()
	}

	// Phase 1: fade the mascot in with its eyes closed.
	for i := 1; i <= 8; i++ {
		if step(AnimState{Blink: true, Shine: -1}, dimPalette(basePalette, float64(i)/8.0), 55*time.Millisecond) {
			return true
		}
	}
	// Phase 2: eyes open; a reflection sweeps across both lenses.
	for shine := -3; shine < 20; shine++ {
		if step(AnimState{EyeX: 0, Shine: shine}, basePalette, 22*time.Millisecond) {
			return true
		}
	}
	// Phase 3: brief settle with a single blink.
	for i := 0; i < 6; i++ {
		st := AnimState{EyeX: 0, Shine: -1}
		if i == 3 {
			st.Blink = true
		}
		if step(st, basePalette, 70*time.Millisecond) {
			return true
		}
	}
	return false
}

// shouldPlay reports whether the intro is appropriate for the current terminal.
// It is intentionally conservative: truecolor, interactive on both ends, and at
// least 80x30, unless disabled by NO_COLOR or GOPHERMIND_INTRO.
func shouldPlay() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GOPHERMIND_INTRO"))) {
	case "0", "off", "false", "no":
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("COLORTERM"))) {
	case "truecolor", "24bit":
	default:
		return false
	}
	if !isTTY(os.Stdin) || !isTTY(os.Stdout) {
		return false
	}
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w < 80 || h < 30 {
		return false
	}
	return true
}

// termWidth returns the current terminal width, or 80 if it cannot be read.
func termWidth() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		return w
	}
	return 80
}

// isTTY reports whether f is a character device (a terminal).
func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && (fi.Mode()&os.ModeCharDevice) != 0
}
