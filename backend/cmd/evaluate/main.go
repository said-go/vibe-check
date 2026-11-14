package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/said-go/vibe-check/backend/internal/evaluation"
	"io"
	"os"
	"text/tabwriter"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("evaluate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	input := flags.String("input", "", "local dataset JSON (required)")
	jsonOutput := flags.Bool("json", false, "write structured report to stdout")
	inputDir := flags.String("input-dir", "", "directory of checkpoint JSON datasets")
	expectedCount := flags.Int("expected-count", 0, "expected number of cases (1..10000); omitted means unknown coverage")
	if flags.Parse(args) != nil {
		return 2
	}
	expectedSet := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "expected-count" {
			expectedSet = true
		}
	})
	if (*input == "") == (*inputDir == "") || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: evaluate (-input dataset.json | -input-dir directory) [-json]")
		return 2
	}
	var d evaluation.Dataset
	var err error
	if *inputDir != "" {
		d, err = evaluation.DecodeDirectory(*inputDir)
	} else {
		var f *os.File
		f, err = os.Open(*input)
		if err == nil {
			d, err = evaluation.Decode(f)
			f.Close()
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	report := evaluation.Run(d)
	if expectedSet {
		if err := report.SetExpectedCount(*expectedCount); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
	}
	if *jsonOutput {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return 2
		}
	} else {
		fmt.Fprintf(stdout, "Dataset: %s. Saved feature scoring only; NOT photo/AI accuracy.\nTotal: %d; matched: %d; failed: %d; invalid features: %d\n", report.Kind, report.Total, report.Passed, report.Failed, report.Invalid)
		if expectedSet {
			fmt.Fprintf(stdout, "Coverage: %s; expected: %d; loaded: %d; missing: %d. Count only; verify case identities separately.\n", report.CoverageStatus, *report.ExpectedCount, report.Total, *report.MissingCount)
		} else {
			fmt.Fprintln(stdout, "Coverage: unknown. Use -expected-count to detect missing cases.")
		}
		w := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "EXPECTED\tACTUAL\tCOUNT")
		for _, c := range report.Confusion {
			fmt.Fprintf(w, "%s\t%s\t%d\n", c.Expected, c.Actual, c.Count)
		}
		w.Flush()
		for _, o := range report.Outcomes {
			if !o.Passed {
				fmt.Fprintf(stdout, "FAIL %q expected=%+v actual=%+v missing=%v unexpected=%v order_mismatch=%t error=%s\n", o.ID, o.Expected, o.Actual, o.MissingComponents, o.UnexpectedComponents, o.ComponentOrderMismatch, o.Error)
			}
		}
	}
	if report.Failed > 0 || report.CoverageStatus == "incomplete" {
		return 1
	}
	return 0
}
