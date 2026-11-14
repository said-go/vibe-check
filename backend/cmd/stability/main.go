// stability is entirely offline; collection is a separate explicitly paid command.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/said-go/vibe-check/backend/internal/evaluation"
	"github.com/said-go/vibe-check/backend/internal/stability"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
func run(args []string, stdout, stderr io.Writer) int {
	fail := func(message string) int { fmt.Fprintln(stderr, message); return 2 }
	if len(args) == 0 || (args[0] != "prepare" && args[0] != "compare") {
		return fail("usage: stability prepare|compare -input file [-output-dir new-directory | -input-dir checkpoints]")
	}
	flags := flag.NewFlagSet("stability", flag.ContinueOnError)
	// Flag parser errors can echo private argument values.
	flags.SetOutput(io.Discard)
	input := flags.String("input", "", "single-image manifest for prepare; saved features dataset for compare")
	dir := flags.String("input-dir", "", "saved checkpoint directory for compare")
	output := flags.String("output-dir", "", "new directory for prepare")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 {
		return fail("invalid arguments")
	}
	if args[0] == "prepare" {
		if *input == "" || *output == "" || *dir != "" {
			return fail("prepare requires -input manifest and -output-dir new-directory")
		}
		f, err := os.Open(*input)
		if err != nil {
			return fail("cannot read manifest")
		}
		m, err := evaluation.DecodeImages(f)
		f.Close()
		if err != nil || len(m.Cases) != 1 {
			return fail("prepare requires a valid manifest with exactly one image and explicit expectation")
		}
		path := m.Cases[0].Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(filepath.Dir(*input), path)
		}
		photo, err := os.Open(path)
		if err != nil {
			return fail("cannot read source image")
		}
		defer photo.Close()
		if err := stability.Prepare(photo, m.Cases[0].Expected, *output); err != nil {
			return fail(err.Error())
		}
		if _, err := fmt.Fprintln(stdout, "Prepared 5 private variants and manifest.json. No AI calls made."); err != nil {
			return 2
		}
		return 0
	}
	if (*input == "") == (*dir == "") || *output != "" {
		return fail("compare requires exactly one of -input dataset or -input-dir checkpoints")
	}
	var d evaluation.Dataset
	var err error
	if *dir != "" {
		d, err = evaluation.DecodeDirectory(*dir)
	} else {
		var f *os.File
		f, err = os.Open(*input)
		if err == nil {
			d, err = evaluation.Decode(f)
			f.Close()
		}
	}
	if err != nil {
		return fail("cannot decode saved dataset")
	}
	report, err := stability.Compare(d)
	if err != nil {
		return fail(err.Error())
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if enc.Encode(report) != nil {
		return fail("cannot write comparison")
	}
	if !report.Stable {
		return 1
	}
	return 0
}
