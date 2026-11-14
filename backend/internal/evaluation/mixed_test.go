package evaluation

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func namedCase(t *testing.T, id string) Case {
	t.Helper()
	for _, c := range fixture(t).Cases {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("missing fixture %s", id)
	return Case{}
}

func TestMixedVerdictsRequireEveryComponent(t *testing.T) {
	for _, tc := range []struct {
		name, fixture  string
		want           Expected
		missing, extra []string
		order          bool
	}{
		{"legacy single cannot accept blend", "synthetic-action-hero-muscular", Expected{Status: "completed", Category: "action_hero"}, nil, []string{"muscular"}, false},
		{"missing second component", "synthetic-action-hero", Expected{Status: "completed", Category: "action_hero", Components: []string{"action_hero", "muscular"}}, []string{"muscular"}, nil, false},
		{"wrong second component", "synthetic-action-hero-muscular", Expected{Status: "completed", Category: "action_hero", Components: []string{"action_hero", "alternative"}}, []string{"alternative"}, []string{"muscular"}, false},
		{"wrong priority", "synthetic-action-hero-muscular", Expected{Status: "completed", Category: "muscular", Components: []string{"muscular", "action_hero"}}, nil, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := namedCase(t, tc.fixture)
			c.Expected = tc.want
			r := Run(Dataset{Kind: "synthetic", Cases: []Case{c}})
			o := r.Outcomes[0]
			if r.Passed != 0 || r.Failed != 1 || r.Invalid != 0 || o.Passed || !reflect.DeepEqual(o.MissingComponents, tc.missing) || !reflect.DeepEqual(o.UnexpectedComponents, tc.extra) || o.ComponentOrderMismatch != tc.order {
				t.Fatalf("wrong diagnosis: %+v", o)
			}
			if r.Confusion[0].Expected == r.Confusion[0].Actual {
				t.Fatal("confusion hid the mixed mismatch")
			}
		})
	}
}

func TestMixedExpectedValidationSharedByDatasetsAndImageManifests(t *testing.T) {
	for _, e := range []Expected{
		{Status: "completed", Category: "action_hero", Components: []string{"action_hero"}},
		{Status: "completed", Category: "action_hero", Components: []string{"action_hero", "muscular", "goth"}},
		{Status: "completed", Category: "action_hero", Components: []string{"action_hero", "action_hero"}},
		{Status: "completed", Category: "action_hero", Components: []string{"muscular", "action_hero"}},
		{Status: "completed", Category: "action_hero", Components: []string{"action_hero", "bad-code"}},
		{Status: "completed", Category: "bad category"},
		{Status: "low_confidence", Reason: "single_person_required", Components: []string{"action_hero", "muscular"}},
	} {
		c := namedCase(t, "synthetic-action-hero")
		c.Expected = e
		data, _ := json.Marshal(Dataset{Kind: "synthetic", Cases: []Case{c}})
		if _, err := Decode(bytes.NewReader(data)); err == nil {
			t.Fatalf("dataset accepted %+v", e)
		}
		data, _ = json.Marshal(ImageManifest{Cases: []ImageCase{{Path: "private-photo.jpg", Expected: e}}})
		if _, err := DecodeImages(bytes.NewReader(data)); err == nil {
			t.Fatalf("manifest accepted %+v", e)
		}
	}
	valid := `{"cases":[{"path":"photo.jpg","expected":{"status":"completed","category":"action_hero","components":["action_hero","muscular"]}}]}`
	m, err := DecodeImages(strings.NewReader(valid))
	if err != nil || !reflect.DeepEqual(m.Cases[0].Expected.Components, []string{"action_hero", "muscular"}) {
		t.Fatalf("valid manifest lost mixed expectation: %+v %v", m, err)
	}
}

func TestReportJSONRetainsActualComponents(t *testing.T) {
	c := namedCase(t, "synthetic-action-hero-muscular")
	r := Run(Dataset{Kind: "synthetic", Cases: []Case{c}})
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip Report
	if err = json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatal(err)
	}
	if !roundtrip.Outcomes[0].Passed || !reflect.DeepEqual(roundtrip.Outcomes[0].Actual.Components, c.Expected.Components) {
		t.Fatalf("missing actual mixture: %s", data)
	}
}
