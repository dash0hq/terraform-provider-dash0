package converter

import (
	"encoding/json"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

// The invariant the conversion rests on: the stored value carries the same data
// the API returned. Order, quoting, and indentation may change; nothing else.
// Otherwise a bug in the key walk or indent rewrite changes the next apply.
func FuzzConvertAPIResponseToYAML(f *testing.F) {
	seeds := []struct {
		apiResponse string
		reference   string
	}{
		{`{"kind":"View","spec":{"title":"a"}}`, "kind: View\nspec:\n  title: b\n"},
		{`{"spec":{"filter":[{"key":"a"},{"key":"b"}]}}`, "spec:\n  filter:\n  - key: a\n"},
		{`{"spec":{"filter":[{"key":"a"}]}}`, "spec:\n  filter:\n    - key: a\n"},
		{`{"spec":{"groups":[{"name":"g","rules":[{"expr":"up","record":"r"}]}]}}`, "spec:\n  groups:\n  - name: g\n    rules:\n    - record: r\n      expr: up\n"},
		{`{"spec":{"routing":{"filters":[[{"key":"team"}]]}}}`, "spec:\n  routing:\n    filters:\n    - - key: team\n"},
		{`{"a":"","b":null,"c":[],"d":{},"e":5,"f":1.5,"g":"5","h":true}`, "a: \"\"\nb: null\n"},
		{`{"expr":"sum(rate(x[5m]))\n- not an item"}`, "expr: |-\n  old\n  text\n"},
		{`{"n":1758000000000,"s":"yes","t":"2026-01-15T10:00:00Z"}`, "n: 1\ns: no\n"},
		{`{"deep":{"a":{"b":{"c":[{"d":[1,2]}]}}}}`, "deep:\n  a:\n    b:\n      c:\n      - d:\n        - 9\n"},
	}
	for _, s := range seeds {
		f.Add(s.apiResponse, s.reference)
	}

	f.Fuzz(func(t *testing.T, apiResponse, reference string) {
		// Only valid JSON is in contract: the wrappers use json.Marshal. Wider
		// inputs report YAML-only cases the API cannot produce.
		if !json.Valid([]byte(apiResponse)) {
			return
		}

		// One parser on both sides, so this compares data, not integer spelling.
		var want interface{}
		if err := yaml.Unmarshal([]byte(apiResponse), &want); err != nil {
			return
		}

		got, err := ConvertAPIResponseToYAML(apiResponse, reference)
		if err != nil {
			return
		}

		var have interface{}
		if err := yaml.Unmarshal([]byte(got), &have); err != nil {
			t.Fatalf("output does not parse as YAML: %v\ninput: %q\nreference: %q\noutput: %q",
				err, apiResponse, reference, got)
		}

		if !reflect.DeepEqual(want, have) {
			t.Fatalf("conversion changed the data\ninput: %q\nreference: %q\noutput: %q\nwant: %#v\ngot:  %#v",
				apiResponse, reference, got, want, have)
		}
	})
}
