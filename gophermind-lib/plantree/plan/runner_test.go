package plan

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"gophermind/gophermind-lib/plantree"
)

// fake is a Completer that records every prompt it is given.
type fake struct {
	prompts []string
	reply   func(n int, prompt string) (string, error)
}

func (f *fake) Complete(_ context.Context, prompt string) (string, error) {
	f.prompts = append(f.prompts, prompt)
	return f.reply(len(f.prompts)-1, prompt)
}

const threePartBrief = "# One\nfirst part text\n\n# Two\nsecond part text\n\n# Three\nthird part text\n"

// opts forces one chunk per section of threePartBrief.
var opts = Options{ProjectName: "demo", ChunkBytes: 30}

const (
	reply1 = `{"phases":[{"title":"Alpha","digest":"first phase","objective":"","tasks":[{"title":"T1","digest":"task one","objective":"","steps":[{"title":"S1","digest":"step one"}]}]}],"overview":"overview 1"}`
	reply2 = `{"phases":[{"title":"alpha","digest":"ignored, already exists","objective":"","tasks":[{"title":"T2","digest":"task two","objective":"","steps":[{"title":"S2","digest":"step two"}]}]}],"overview":"overview 2"}`
	reply3 = `{"phases":[{"title":"Beta","digest":"second phase","objective":"","tasks":[{"title":"T3","digest":"task three","objective":"","steps":[{"title":"S3","digest":"step three"}]}]}],"overview":"overview 3"}`
)

// byChunk answers each chunk's pass with the reply written for it.
func byChunk(_ int, prompt string) (string, error) {
	switch {
	case strings.Contains(prompt, "first part text"):
		return reply1, nil
	case strings.Contains(prompt, "second part text"):
		return reply2, nil
	case strings.Contains(prompt, "third part text"):
		return reply3, nil
	}
	return "", errors.New("prompt has no known chunk")
}

var wantTree = []string{
	"plan demo",
	"phase-001 Alpha",
	"phase-001.task-001 T1",
	"phase-001.task-001.step-001 S1",
	"phase-001.task-002 T2",
	"phase-001.task-002.step-001 S2",
	"phase-002 Beta",
	"phase-002.task-001 T3",
	"phase-002.task-001.step-001 S3",
}

func TestRunPass1BuildsTheWholeSkeleton(t *testing.T) {
	r := plantree.Open(t.TempDir())
	f := &fake{reply: byChunk}
	res, err := RunPass1(context.Background(), r, threePartBrief, f, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Chunks != 3 || res.Processed != 3 || res.Created != (Created{Phases: 2, Tasks: 3, Steps: 3}) {
		t.Errorf("Result = %+v", res)
	}
	if got := ids(t, r); !reflect.DeepEqual(got, wantTree) {
		t.Errorf("tree = %v", got)
	}
	if ov, _ := ReadOverview(r.Dir()); ov != "overview 3\n" {
		t.Errorf("overview = %q", ov)
	}
	if st, found, _ := loadState(r); !found || st.Next != 3 {
		t.Errorf("state = %+v, found=%v", st, found)
	}
	acts, err := r.NextActions()
	if err != nil || len(acts.Runnable) != 3 || acts.Runnable[0].Kind != plantree.ActionDraft {
		t.Errorf("NextActions = %+v, %v; want three draft actions", acts, err)
	}
	if err := r.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

func TestEveryPassIsFreshAndBounded(t *testing.T) {
	r := plantree.Open(t.TempDir())
	f := &fake{reply: byChunk}
	if _, err := RunPass1(context.Background(), r, threePartBrief, f, opts); err != nil {
		t.Fatal(err)
	}
	if len(f.prompts) != 3 {
		t.Fatalf("%d prompts, want 3", len(f.prompts))
	}
	if strings.Contains(f.prompts[1], "first part text") || strings.Contains(f.prompts[1], "third part text") {
		t.Error("the second prompt carries text from another chunk")
	}
	if !strings.Contains(f.prompts[1], "overview 1") || !strings.Contains(f.prompts[1], "- Phase: Alpha") {
		t.Error("the second prompt must carry the running overview and the outline so far")
	}
	if strings.Contains(f.prompts[0], "overview 1") {
		t.Error("the first prompt cannot know the later overview")
	}
	for i, p := range f.prompts {
		if len(p) > 4000 {
			t.Errorf("prompt %d is %d bytes for a tiny brief", i, len(p))
		}
	}
}

func TestResumeAfterAFailureContinuesWhereItStopped(t *testing.T) {
	dir := t.TempDir()
	r := plantree.Open(dir)
	broken := &fake{reply: func(n int, p string) (string, error) {
		if strings.Contains(p, "second part text") {
			return "", errors.New("model unavailable")
		}
		return byChunk(n, p)
	}}
	res, err := RunPass1(context.Background(), r, threePartBrief, broken, opts)
	if err == nil || !strings.Contains(err.Error(), "chunk 2 of 3") || !strings.Contains(err.Error(), "model unavailable") {
		t.Fatalf("err = %v, want it to name chunk 2 of 3 and the cause", err)
	}
	if res.Processed != 1 {
		t.Errorf("Processed = %d, want 1", res.Processed)
	}
	if st, _, _ := loadState(r); st.Next != 1 {
		t.Errorf("state.Next = %d, want 1", st.Next)
	}

	good := &fake{reply: byChunk} // a new process, a new model client
	res, err = RunPass1(context.Background(), plantree.Open(dir), threePartBrief, good, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(good.prompts) != 2 || res.Processed != 2 {
		t.Errorf("resume made %d calls, Processed=%d; want 2 and 2", len(good.prompts), res.Processed)
	}
	if got := ids(t, plantree.Open(dir)); !reflect.DeepEqual(got, wantTree) {
		t.Errorf("tree after resume = %v", got)
	}
}

func TestReplayOfAMergedButUnrecordedChunkAddsNothingTwice(t *testing.T) {
	r := plantree.Open(t.TempDir())
	// Simulate a crash after chunk 1 was merged but before the cursor moved.
	if err := ensureRoot(r, "demo"); err != nil {
		t.Fatal(err)
	}
	first, err := ParsePass1(reply1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Merge(r, first); err != nil {
		t.Fatal(err)
	}
	if _, err := RunPass1(context.Background(), r, threePartBrief, &fake{reply: byChunk}, opts); err != nil {
		t.Fatal(err)
	}
	if got := ids(t, r); !reflect.DeepEqual(got, wantTree) {
		t.Errorf("tree = %v", got)
	}
}

func TestMalformedReplyGetsOneCorrectionRetry(t *testing.T) {
	r := plantree.Open(t.TempDir())
	f := &fake{reply: func(n int, p string) (string, error) {
		if n == 0 {
			return "Sure! Here is a plan, but no JSON.", nil
		}
		return byChunk(n, p)
	}}
	res, err := RunPass1(context.Background(), r, threePartBrief, f, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.prompts) != 4 || res.Processed != 3 {
		t.Errorf("%d calls, Processed=%d; want 4 and 3", len(f.prompts), res.Processed)
	}
	if !strings.Contains(f.prompts[1], "rejected") || !strings.Contains(f.prompts[1], "no JSON object") {
		t.Errorf("the retry prompt must say why: %q", f.prompts[1][len(f.prompts[1])-200:])
	}
}

func TestReplyRejectedTwiceStopsWithoutAdvancing(t *testing.T) {
	r := plantree.Open(t.TempDir())
	f := &fake{reply: func(int, string) (string, error) { return "still not json", nil }}
	_, err := RunPass1(context.Background(), r, threePartBrief, f, opts)
	if err == nil || !strings.Contains(err.Error(), "rejected twice") || !strings.Contains(err.Error(), "chunk 1 of 3") {
		t.Fatalf("err = %v", err)
	}
	if len(f.prompts) != 2 {
		t.Errorf("%d calls, want exactly 2 (ask, then one correction)", len(f.prompts))
	}
	if st, _, _ := loadState(r); st.Next != 0 {
		t.Errorf("state.Next = %d, want 0", st.Next)
	}
}

func TestResumingWithADifferentBriefIsRefused(t *testing.T) {
	dir := t.TempDir()
	r := plantree.Open(dir)
	if _, err := RunPass1(context.Background(), r, threePartBrief, &fake{reply: byChunk}, opts); err != nil {
		t.Fatal(err)
	}
	f := &fake{reply: byChunk}
	if _, err := RunPass1(context.Background(), r, threePartBrief+"one more line\n", f, opts); !errors.Is(err, ErrBriefChanged) {
		t.Errorf("changed brief: err = %v, want ErrBriefChanged", err)
	}
	other := opts
	other.ChunkBytes = 60
	if _, err := RunPass1(context.Background(), r, threePartBrief, f, other); !errors.Is(err, ErrBriefChanged) {
		t.Errorf("changed chunk size: err = %v, want ErrBriefChanged", err)
	}
	if len(f.prompts) != 0 {
		t.Errorf("refused runs must not call the model, made %d calls", len(f.prompts))
	}
}

func TestOverviewOverTheCapIsCompressedThenCut(t *testing.T) {
	long := strings.Repeat("overview sentence. ", 20) // 380 bytes
	brief := "# Only\nsome text\n"
	mk := func(compressed string) *fake {
		return &fake{reply: func(_ int, p string) (string, error) {
			if strings.Contains(p, "Compress this project overview") {
				return compressed, nil
			}
			return fmt.Sprintf(`{"phases":[],"overview":%q}`, long), nil
		}}
	}
	o := Options{ProjectName: "demo", OverviewCap: 100}

	r := plantree.Open(t.TempDir())
	if _, err := RunPass1(context.Background(), r, brief, mk("short overview"), o); err != nil {
		t.Fatal(err)
	}
	if ov, _ := ReadOverview(r.Dir()); ov != "short overview\n" {
		t.Errorf("compressed overview = %q", ov)
	}

	r2 := plantree.Open(t.TempDir())
	if _, err := RunPass1(context.Background(), r2, brief, mk(long), o); err != nil {
		t.Fatal(err)
	}
	ov, _ := ReadOverview(r2.Dir())
	if len(ov) > 101 || !strings.Contains(ov, "[overview truncated]") {
		t.Errorf("a compression that is still too long must be cut: len=%d %q", len(ov), ov)
	}
}

func TestEmptyBriefAndCancelledContext(t *testing.T) {
	r := plantree.Open(t.TempDir())
	f := &fake{reply: byChunk}
	if _, err := RunPass1(context.Background(), r, "  \n", f, opts); err == nil {
		t.Error("an empty brief must be an error")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RunPass1(ctx, r, threePartBrief, f, opts); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled run: err = %v", err)
	}
	if len(f.prompts) != 0 {
		t.Errorf("no model calls expected, made %d", len(f.prompts))
	}
}

func TestResumingWithADifferentChunkSizeButTheSameChunkCountIsRefused(t *testing.T) {
	dir := t.TempDir()
	r := plantree.Open(dir)
	broken := &fake{reply: func(n int, p string) (string, error) {
		if n >= 1 {
			return "", errors.New("model unavailable")
		}
		return byChunk(n, p)
	}}
	res, err := RunPass1(context.Background(), r, threePartBrief, broken, opts)
	if err == nil || !strings.Contains(err.Error(), "chunk 2 of 3") {
		t.Fatalf("err = %v, want it to name chunk 2 of 3", err)
	}
	if res.Processed != 1 {
		t.Errorf("Processed = %d, want 1", res.Processed)
	}

	other := opts
	other.ChunkBytes = 40
	good := &fake{reply: byChunk}
	if _, err := RunPass1(context.Background(), r, threePartBrief, good, other); !errors.Is(err, ErrBriefChanged) {
		t.Errorf("changed chunk size with same count: err = %v, want ErrBriefChanged", err)
	}
	if len(good.prompts) != 0 {
		t.Errorf("refused runs must not call the model, made %d calls", len(good.prompts))
	}
}
