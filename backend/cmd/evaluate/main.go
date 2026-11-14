package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/said-go/vibe-check/backend/internal/evaluation"
	"os"
	"text/tabwriter"
)

func main() { os.Exit(run()) }
func run() int {
	input := flag.String("input", "", "local dataset JSON (required)")
	jsonOutput := flag.Bool("json", false, "write structured report to stdout")
	flag.Parse()
	if *input == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: evaluate -input dataset.json [-json]")
		return 2
	}
	f, err := os.Open(*input)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer f.Close()
	d, err := evaluation.Decode(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	report := evaluation.Run(d)
	if *jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return 2
		}
	} else {
		fmt.Printf("Dataset: %s. Saved feature scoring only; NOT photo/AI accuracy.\nTotal: %d; matched: %d; failed: %d; invalid features: %d\n", report.Kind, report.Total, report.Passed, report.Failed, report.Invalid)
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "EXPECTED\tACTUAL\tCOUNT")
		for _, c := range report.Confusion {
			fmt.Fprintf(w, "%s\t%s\t%d\n", c.Expected, c.Actual, c.Count)
		}
		w.Flush()
		for _, o := range report.Outcomes {
			if !o.Passed {
				fmt.Printf("FAIL %q expected=%+v actual=%+v error=%s\n", o.ID, o.Expected, o.Actual, o.Error)
			}
		}
	}
	if report.Failed > 0 {
		return 1
	}
	return 0
}
