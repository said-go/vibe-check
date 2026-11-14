// Package evaluation checks saved feature contracts against explicit expectations.
// It does not call AI or measure image recognition accuracy.
package evaluation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/said-go/vibe-check/backend/internal/analysis"
	"github.com/said-go/vibe-check/backend/internal/scoring"
)

type Expected struct {
	Status     string   `json:"status"`
	Category   string   `json:"category"`
	Reason     string   `json:"reason,omitempty"`
	Components []string `json:"components,omitempty"`
}
type Case struct {
	ID       string          `json:"id"`
	Features json.RawMessage `json:"features"`
	Expected Expected        `json:"expected"`
}
type Dataset struct {
	Kind  string `json:"kind"`
	Cases []Case `json:"cases"`
}
type Outcome struct {
	ID                     string   `json:"id"`
	Expected               Expected `json:"expected"`
	Actual                 Expected `json:"actual"`
	Passed                 bool     `json:"passed"`
	Error                  string   `json:"error,omitempty"`
	MissingComponents      []string `json:"missing_components,omitempty"`
	UnexpectedComponents   []string `json:"unexpected_components,omitempty"`
	ComponentOrderMismatch bool     `json:"component_order_mismatch,omitempty"`
}
type Cell struct {
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Count    int    `json:"count"`
}
type Report struct {
	CoverageStatus string    `json:"coverage_status"`
	ExpectedCount  *int      `json:"expected_count"`
	MissingCount   *int      `json:"missing_count"`
	Kind           string    `json:"kind"`
	Total          int       `json:"total"`
	Passed         int       `json:"passed"`
	Failed         int       `json:"failed"`
	Invalid        int       `json:"invalid"`
	Outcomes       []Outcome `json:"outcomes"`
	Confusion      []Cell    `json:"confusion"`
}

// SetExpectedCount checks coverage separately from the verdict matches.
// A count cannot prove that these are the intended cases; IDs must still be reviewed.
func (r *Report) SetExpectedCount(expected int) error {
	if expected < 1 || expected > 10000 || r.Total > expected {
		return fmt.Errorf("expected-count must be 1..10000 and at least the loaded case count")
	}
	missing := expected - r.Total
	r.ExpectedCount, r.MissingCount = &expected, &missing
	r.CoverageStatus = "complete"
	if missing > 0 {
		r.CoverageStatus = "incomplete"
	}
	return nil
}

func Decode(r io.Reader) (Dataset, error) {
	var d Dataset
	raw, err := io.ReadAll(io.LimitReader(r, (8<<20)+1))
	if err != nil {
		return d, err
	}
	if len(raw) > 8<<20 {
		return d, fmt.Errorf("dataset exceeds 8 MiB")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return d, err
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return d, fmt.Errorf("trailing JSON")
	}
	if d.Kind != "synthetic" && d.Kind != "reviewed_features" {
		return d, fmt.Errorf("kind must be synthetic or reviewed_features")
	}
	if len(d.Cases) == 0 || len(d.Cases) > 10000 {
		return d, fmt.Errorf("expected 1..10000 cases")
	}
	seen := map[string]bool{}
	for _, c := range d.Cases {
		if strings.TrimSpace(c.ID) == "" || seen[c.ID] {
			return d, fmt.Errorf("missing or duplicate case id")
		}
		seen[c.ID] = true
		if !validExpected(c.Expected) {
			return d, fmt.Errorf("case %q: inconsistent expectation", c.ID)
		}
	}
	return d, nil
}

func Run(d Dataset) Report {
	report := Report{CoverageStatus: "unknown", Kind: d.Kind, Total: len(d.Cases), Outcomes: []Outcome{}, Confusion: []Cell{}}
	counts := map[[2]string]int{}
	engine := scoring.NewEngine()
	for _, c := range d.Cases {
		o := Outcome{ID: c.ID, Expected: c.Expected}
		f, err := analysis.DecodeFeatures(bytes.NewReader(c.Features))
		if err == nil {
			var result scoring.Result
			result, err = engine.Evaluate(f)
			o.Actual = Expected{Status: result.Status, Category: result.PrimaryCategory.Code, Reason: result.LowConfidenceReason}
			for _, component := range result.Components {
				o.Actual.Components = append(o.Actual.Components, component.Code)
			}
		}
		if err != nil {
			o.Error = err.Error()
			report.Invalid++
			o.Actual.Status = "invalid_features"
		}
		o.Passed = err == nil && o.Actual.Status == o.Expected.Status && o.Actual.Category == o.Expected.Category && o.Actual.Reason == o.Expected.Reason && slices.Equal(o.Actual.Components, o.Expected.Components)
		if err == nil {
			want, got := categories(o.Expected), categories(o.Actual)
			for _, code := range want {
				if !slices.Contains(got, code) {
					o.MissingComponents = append(o.MissingComponents, code)
				}
			}
			for _, code := range got {
				if !slices.Contains(want, code) {
					o.UnexpectedComponents = append(o.UnexpectedComponents, code)
				}
			}
			o.ComponentOrderMismatch = len(want) > 1 && len(want) == len(got) && len(o.MissingComponents) == 0 && len(o.UnexpectedComponents) == 0 && !slices.Equal(want, got)
		}
		if o.Passed {
			report.Passed++
		} else {
			report.Failed++
		}
		counts[[2]string{label(o.Expected), label(o.Actual)}]++
		report.Outcomes = append(report.Outcomes, o)
	}
	for pair, n := range counts {
		report.Confusion = append(report.Confusion, Cell{pair[0], pair[1], n})
	}
	sort.Slice(report.Confusion, func(i, j int) bool {
		a, b := report.Confusion[i], report.Confusion[j]
		if a.Expected == b.Expected {
			return a.Actual < b.Actual
		}
		return a.Expected < b.Expected
	})
	return report
}
func label(e Expected) string {
	if e.Status == "completed" {
		if len(e.Components) > 0 {
			return strings.Join(e.Components, "+")
		}
		return e.Category
	}
	if e.Reason != "" {
		return e.Status + ":" + e.Reason
	}
	return e.Status
}

var categoryToken = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func validExpected(e Expected) bool {
	if e.Status == "low_confidence" {
		return e.Category == "" && categoryToken.MatchString(e.Reason) && len(e.Components) == 0
	}
	if e.Status != "completed" || !categoryToken.MatchString(e.Category) || e.Reason != "" {
		return false
	}
	if len(e.Components) == 0 {
		return true
	}
	return len(e.Components) == 2 && e.Components[0] == e.Category && e.Components[0] != e.Components[1] && categoryToken.MatchString(e.Components[1])
}

func categories(e Expected) []string {
	if e.Status != "completed" {
		return nil
	}
	if len(e.Components) > 0 {
		return e.Components
	}
	return []string{e.Category}
}
