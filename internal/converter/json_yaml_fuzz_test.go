package converter

import (
	"encoding/json"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

// FuzzConvertAPIResponseToYAML pins the one invariant the whole conversion rests
// on: the value stored in state must carry the same data the API returned. Key
// order, quoting, and indentation may change. Nothing else may.
//
// This is what stops a subtle bug in the key-order walk or the sequence-indent
// rewrite from silently changing a document, which would send the wrong body on
// the next apply.
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
		// The client wrappers build this input with json.Marshal, so only valid
		// JSON is in contract. Fuzzing wider than that reports differences YAML
		// allows and the API can never produce, such as a null mapping key.
		if !json.Valid([]byte(apiResponse)) {
			return
		}

		// Parse both sides with the same parser, so the comparison is about the
		// data and not about how two libraries spell an integer.
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
