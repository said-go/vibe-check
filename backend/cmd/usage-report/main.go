package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/said-go/vibe-check/backend/internal/usage"
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }
func run(args []string, in io.Reader, out, diagnostic io.Writer) int {
	flags := flag.NewFlagSet("usage-report", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	input := flags.String("input", "-", "JSON-lines file or - for stdin")
	tariffPath := flags.String("tariff", "", "explicit versioned RUB tariff JSON")
	model := flags.String("assume-model", "", "assert all input attempts use this model (required with tariff)")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		fmt.Fprintln(diagnostic, "Usage: usage-report [-input server.jsonl]; default reads stdin")
		return 2
	}
	if (*tariffPath == "") != (*model == "") {
		fmt.Fprintln(diagnostic, "Use -tariff together with -assume-model; logs do not identify models")
		return 2
	}
	var tariff usage.Tariff
	if *tariffPath != "" {
		f, err := os.Open(*tariffPath)
		if err != nil {
			fmt.Fprintln(diagnostic, "Cannot open tariff")
			return 2
		}
		tariff, err = usage.DecodeTariff(f)
		f.Close()
		if err != nil {
			fmt.Fprintln(diagnostic, err)
			return 2
		}
	}
	if *input != "-" {
		f, err := os.Open(*input)
		if err != nil {
			fmt.Fprintln(diagnostic, "Cannot open log input")
			return 2
		}
		defer f.Close()
		in = f
	}
	report := usage.Read(in)
	if *tariffPath != "" {
		estimate, err := usage.EstimateCost(report, tariff, *model)
		if err != nil {
			fmt.Fprintln(diagnostic, err)
			return 2
		}
		report.Estimate = &estimate
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if encoder.Encode(report) != nil {
		fmt.Fprintln(diagnostic, "Cannot write aggregate report")
		return 2
	}
	if report.InputIncomplete || report.InvalidLines > 0 || report.Estimate != nil && report.Estimate.Partial {
		fmt.Fprintln(diagnostic, "Report is partial: some attempts are unpriced or input is invalid/incomplete; inspect counts. Raw records are not printed.")
		return 1
	}
	return 0
}
