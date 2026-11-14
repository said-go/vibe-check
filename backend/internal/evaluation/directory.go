package evaluation

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// DecodeDirectory combines top-level checkpoint datasets in filename order.
// It never follows symlinks or silently merges duplicate case identifiers.
func DecodeDirectory(path string) (Dataset, error) {
	out := Dataset{}
	entries, err := os.ReadDir(path)
	if err != nil {
		return out, fmt.Errorf("cannot read input directory")
	}
	seen := map[string]bool{}
	var total int64
	files := 0
	for _, entry := range entries {
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}
		files++
		if files > 10000 {
			return Dataset{}, fmt.Errorf("too many dataset files")
		}
		info, e := entry.Info()
		if e != nil || !info.Mode().IsRegular() {
			return Dataset{}, fmt.Errorf("dataset file %q must be a readable regular file", entry.Name())
		}
		if info.Size() > (8<<20)-total {
			return Dataset{}, fmt.Errorf("combined datasets exceed 8 MiB")
		}
		f, e := os.Open(filepath.Join(path, entry.Name()))
		if e != nil {
			return Dataset{}, fmt.Errorf("cannot read dataset file %q", entry.Name())
		}
		raw, e := io.ReadAll(io.LimitReader(f, (8<<20)-total+1))
		f.Close()
		if e != nil {
			return Dataset{}, fmt.Errorf("cannot read dataset file %q", entry.Name())
		}
		total += int64(len(raw))
		if total > 8<<20 {
			return Dataset{}, fmt.Errorf("combined datasets exceed 8 MiB")
		}
		d, e := Decode(bytes.NewReader(raw))
		if e != nil {
			return Dataset{}, fmt.Errorf("invalid dataset file %q: %w", entry.Name(), e)
		}
		if out.Kind != "" && out.Kind != d.Kind {
			return Dataset{}, fmt.Errorf("cannot mix synthetic and reviewed_features datasets")
		}
		out.Kind = d.Kind
		for _, c := range d.Cases {
			if seen[c.ID] {
				return Dataset{}, fmt.Errorf("duplicate case id %q across datasets; evaluate separate runs separately", c.ID)
			}
			seen[c.ID] = true
			out.Cases = append(out.Cases, c)
			if len(out.Cases) > 10000 {
				return Dataset{}, fmt.Errorf("combined datasets exceed 10000 cases")
			}
		}
	}
	if files == 0 {
		return Dataset{}, fmt.Errorf("input directory contains no JSON datasets")
	}
	return out, nil
}
