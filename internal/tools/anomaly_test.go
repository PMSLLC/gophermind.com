package tools

import (
	"strings"
	"testing"
)

func TestDetectAnomaliesFindsOutlier(t *testing.T) {
	// A tight cluster with one large spike.
	out, err := run(t, DetectAnomalies(), `{"values":[10,11,9,10,12,10,200]}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "200") {
		t.Errorf("the spike (200) should be flagged:\n%s", out)
	}
	// index 6 is the outlier.
	if !strings.Contains(out, "6") {
		t.Errorf("outlier index should be reported:\n%s", out)
	}
}

func TestDetectAnomaliesNone(t *testing.T) {
	out, err := run(t, DetectAnomalies(), `{"values":[10,11,9,10,12,10]}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(out), "no ") && !strings.Contains(strings.ToLower(out), "none") {
		t.Errorf("a uniform series should report no anomalies:\n%s", out)
	}
}

func TestDetectAnomaliesSensitivity(t *testing.T) {
	// A lower threshold flags a milder deviation that the default would miss.
	mild := `{"values":[10,11,9,10,12,10,20],"threshold":2.0}`
	out, err := run(t, DetectAnomalies(), mild)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "20") {
		t.Errorf("with a lower threshold, 20 should be flagged:\n%s", out)
	}
}

func TestDetectAnomaliesTooFew(t *testing.T) {
	if _, err := run(t, DetectAnomalies(), `{"values":[1,2]}`); err == nil {
		t.Error("too few points to compute a meaningful deviation should error")
	}
}
