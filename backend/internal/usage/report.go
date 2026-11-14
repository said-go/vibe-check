// Package usage aggregates provider attempt logs without retaining raw log data.
package usage

import (
	"bufio"
	"encoding/json"
	"io"
	"math"
	"sort"
)

const MaxLineBytes = 1 << 20

type Group struct {
	Model        string `json:"model,omitempty"`
	Provider     string `json:"provider"`
	Outcome      string `json:"outcome"`
	Attempts     int64  `json:"attempts"`
	UnknownUsage int64  `json:"unknown_usage"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	TotalTokens  int64  `json:"total_tokens"`
}
type Report struct {
	Estimate        *Estimate `json:"estimate,omitempty"`
	Attempts        int64     `json:"attempts"`
	UnknownUsage    int64     `json:"unknown_usage"`
	InputTokens     int64     `json:"input_tokens"`
	OutputTokens    int64     `json:"output_tokens"`
	TotalTokens     int64     `json:"total_tokens"`
	SkippedLines    int64     `json:"skipped_lines"`
	InvalidLines    int64     `json:"invalid_lines"`
	InputIncomplete bool      `json:"input_incomplete"`
	Groups          []Group   `json:"groups"`
}

// Read accepts JSON-lines emitted by the server. Invalid lines are counted,
// never echoed. A scanner failure stops processing and marks the report partial.
func Read(r io.Reader) Report {
	report := Report{Groups: []Group{}}
	groups := map[[3]string]*Group{}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), MaxLineBytes)
	for scanner.Scan() {
		var envelope struct {
			Message string `json:"msg"`
		}
		if json.Unmarshal(scanner.Bytes(), &envelope) != nil {
			report.InvalidLines++
			continue
		}
		if envelope.Message != "provider_attempt" {
			report.SkippedLines++
			continue
		}
		var row struct {
			Model     string `json:"model"`
			Provider  string `json:"provider"`
			Outcome   string `json:"outcome"`
			Available *bool  `json:"usage_available"`
			Input     *int64 `json:"input_tokens"`
			Output    *int64 `json:"output_tokens"`
			Total     *int64 `json:"total_tokens"`
		}
		if json.Unmarshal(scanner.Bytes(), &row) != nil || (row.Model != "" && !identifier.MatchString(row.Model)) || (row.Provider != "yandex" && row.Provider != "openai") || !validOutcome(row.Outcome) {
			report.InvalidLines++
			continue
		}
		known := row.Available != nil && *row.Available
		if known && (!validTokens(row.Input) || !validTokens(row.Output) || !validTokens(row.Total)) {
			report.InvalidLines++
			continue
		}
		if known && (report.InputTokens > math.MaxInt64-*row.Input || report.OutputTokens > math.MaxInt64-*row.Output || report.TotalTokens > math.MaxInt64-*row.Total) {
			report.InvalidLines++
			continue
		}
		key := [3]string{row.Provider, row.Outcome, row.Model}
		g := groups[key]
		if g == nil {
			g = &Group{Provider: row.Provider, Outcome: row.Outcome, Model: row.Model}
			groups[key] = g
		}
		report.Attempts++
		g.Attempts++
		if !known {
			report.UnknownUsage++
			g.UnknownUsage++
			continue
		}
		report.InputTokens += *row.Input
		report.OutputTokens += *row.Output
		report.TotalTokens += *row.Total
		g.InputTokens += *row.Input
		g.OutputTokens += *row.Output
		g.TotalTokens += *row.Total
	}
	if scanner.Err() != nil {
		report.InputIncomplete = true
	}
	for _, g := range groups {
		report.Groups = append(report.Groups, *g)
	}
	sort.Slice(report.Groups, func(i, j int) bool {
		a, b := report.Groups[i], report.Groups[j]
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		return a.Outcome < b.Outcome
	})
	return report
}
func validTokens(n *int64) bool { return n != nil && *n >= 0 && *n <= 1_000_000_000 }
func validOutcome(s string) bool {
	switch s {
	case "success", "cancelled", "timeout", "rejected", "unavailable", "invalid_response":
		return true
	}
	return false
}
