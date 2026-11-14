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
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		fmt.Fprintln(diagnostic, "Usage: usage-report [-input server.jsonl]; default reads stdin")
		return 2
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
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if encoder.Encode(report) != nil {
		fmt.Fprintln(diagnostic, "Cannot write aggregate report")
		return 2
	}
	if report.InputIncomplete || report.InvalidLines > 0 {
		fmt.Fprintln(diagnostic, "Report is partial: invalid records or unreadable/oversized input; inspect counts. Raw records are not printed.")
		return 1
	}
	return 0
}
