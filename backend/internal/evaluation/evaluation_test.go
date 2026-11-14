package evaluation

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func fixture(t *testing.T) Dataset {
	t.Helper()
	raw, err := os.ReadFile("testdata/synthetic.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func TestSyntheticExamples(t *testing.T) {
	r := Run(fixture(t))
	if r.Total != 5 || r.Passed != 5 || r.Failed != 0 {
		t.Fatalf("%+v", r)
	}
}
func TestWrongExpectationAndInvalidContractAreFailures(t *testing.T) {
	d := fixture(t)
	d.Cases[0].Expected.Category = "goth"
	d.Cases[1].Features = json.RawMessage(`{}`)
	r := Run(d)
	if r.Failed != 2 || r.Invalid != 1 || r.Passed != 3 {
		t.Fatalf("%+v", r)
	}
	if r.Outcomes[0].Actual.Category != "healthy_casual" || r.Outcomes[1].Error == "" {
		t.Fatal("missing diagnosis")
	}
	total := 0
	for _, cell := range r.Confusion {
		total += cell.Count
	}
	if total != r.Total {
		t.Fatal("confusion omits failures")
	}
}
func TestRejectAmbiguousDatasets(t *testing.T) {
	for _, kind := range []string{"empty", "duplicate", "status", "inconsistent", "unknown", "trailing"} {
		t.Run(kind, func(t *testing.T) {
			d := fixture(t)
			switch kind {
			case "empty":
				d.Cases = nil
			case "duplicate":
				d.Cases[1].ID = d.Cases[0].ID
			case "status":
				d.Cases[0].Expected.Status = "done"
			case "inconsistent":
				d.Cases[0].Expected.Category = ""
			}
			raw, _ := json.Marshal(d)
			if kind == "unknown" {
				raw = append([]byte(`{"extra":1,`), raw[1:]...)
			}
			if kind == "trailing" {
				raw = append(raw, []byte(` {}`)...)
			}
			if _, err := Decode(bytes.NewReader(raw)); err == nil {
				t.Fatal("accepted invalid dataset")
			}
		})
	}
}
