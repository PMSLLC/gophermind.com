package plan

import "fmt"

// Lower bounds on the tunable sizes. Zero always means "use the default", so
// these only reject a value a caller chose. They are the points below which a
// setting stops meaning what its name says rather than merely being small:
//
//   - minOverviewCap: FitOverview appends a 21 byte marker when it cuts, so
//     below 22 it returns more bytes than the cap it was given.
//   - minBriefBytes: Excerpts appends a 32 byte marker when it cuts.
//   - a negative size is always a mistake, never "smaller".
const (
	minOverviewCap = 32
	minBriefBytes  = 64
)

// WithDefaults returns these options with every unset size replaced by the
// default RunPass1 would apply. A caller that has to report or reason about
// the sizes a run will really use asks for this rather than repeating the
// defaulting rules.
func (o Options) WithDefaults() Options {
	if o.ChunkBytes < 1 {
		o.ChunkBytes = DefaultChunkBytes
	}
	if o.OverviewCap < 1 {
		o.OverviewCap = OverviewCapBytes
	}
	if o.ProjectName == "" {
		o.ProjectName = "project"
	}
	return o
}

// WithDefaults returns these options with every unset size replaced by the
// default RunPass2 would apply. ProjectName is left alone: its default is the
// tree's root title, which needs the tree.
func (o Options2) WithDefaults() Options2 {
	if o.StepsPerPass < 1 {
		o.StepsPerPass = defaultStepsPerPass
	}
	if o.BriefBytes < 1 {
		o.BriefBytes = defaultBriefBytes
	}
	return o
}

// Validate reports why these pass-1 options cannot be used, or nil.
func (o Options) Validate() error {
	if o.ChunkBytes < 0 {
		return fmt.Errorf("plan: Options.ChunkBytes is %d; use 0 for the default (%d)", o.ChunkBytes, DefaultChunkBytes)
	}
	if o.OverviewCap < 0 || (o.OverviewCap > 0 && o.OverviewCap < minOverviewCap) {
		return fmt.Errorf("plan: Options.OverviewCap is %d; use 0 for the default (%d) or at least %d, below which a cut overview is larger than its cap", o.OverviewCap, OverviewCapBytes, minOverviewCap)
	}
	return nil
}

// Validate reports why these pass-2 options cannot be used, or nil.
func (o Options2) Validate() error {
	if o.StepsPerPass < 0 {
		return fmt.Errorf("plan: Options2.StepsPerPass is %d; use 0 for the default (%d)", o.StepsPerPass, defaultStepsPerPass)
	}
	if o.BriefBytes < 0 || (o.BriefBytes > 0 && o.BriefBytes < minBriefBytes) {
		return fmt.Errorf("plan: Options2.BriefBytes is %d; use 0 for the default (%d) or at least %d, below which a cut excerpt is larger than its budget", o.BriefBytes, defaultBriefBytes, minBriefBytes)
	}
	return nil
}
