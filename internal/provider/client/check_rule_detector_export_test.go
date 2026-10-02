package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dash0 "github.com/dash0hq/dash0-api-client-go"
	"github.com/dash0hq/terraform-provider-dash0/internal/converter"
)

func TestCheckRuleDetectorExportLifecycle(t *testing.T) {
	paths, err := filepath.Glob("testdata/detector-export/*.yaml")
	require.NoError(t, err)
	require.Len(t, paths, 7)
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			input, err := os.ReadFile(path)
			require.NoError(t, err)
			var stored dash0.PrometheusAlertRule
			puts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "production", r.URL.Query().Get("dataset"))
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodPut {
					puts++
					require.NoError(t, json.NewDecoder(r.Body).Decode(&stored))
					require.NotNil(t, stored.Thresholds)
					annotations := stored.Annotations.AdditionalProperties
					if direction, ok := annotations["dash0.com/baseline-direction"]; ok {
						stored.Thresholds.Baseline = &dash0.CheckThresholdBaseline{Direction: dash0.AnomalyDirection(direction), SpreadFloor: dash0.Ptr(0.1)}
					} else {
						value := 2.0
						if annotations["dash0.com/change-gate-comparison"] == "absolute_delta" {
							value = 0.05
						}
						stored.Thresholds.ChangeGate = &dash0.CheckThresholdChangeGate{Comparison: dash0.ChangeGateComparison(annotations["dash0.com/change-gate-comparison"]), Value: value, BaselineWindow: dash0.Duration(annotations["dash0.com/change-gate-baseline-window"])}
					}
					for _, key := range []string{"dash0.com/baseline-direction", "dash0.com/baseline-spread-floor", "dash0.com/change-gate-comparison", "dash0.com/change-gate-value", "dash0.com/change-gate-baseline-window"} {
						delete(annotations, key)
					}
				} else {
					assert.Equal(t, http.MethodGet, r.Method)
				}
				require.NoError(t, json.NewEncoder(w).Encode(stored))
			}))
			t.Cleanup(server.Close)
			inner, err := dash0.NewClient(dash0.WithApiUrl(server.URL), dash0.WithAuthToken("auth_test-token"))
			require.NoError(t, err)
			c := &dash0Client{inner: inner}
			require.NoError(t, c.CreateCheckRule(t.Context(), "tf_detector", string(input), "production"))
			read, err := c.GetCheckRule(t.Context(), "tf_detector", "production")
			require.NoError(t, err)
			equivalent, err := converter.ResourceYAMLEquivalent(string(input), read, []string{"metadata.name"}, nil)
			require.NoError(t, err)
			assert.True(t, equivalent, read)
			require.NoError(t, c.UpdateCheckRule(t.Context(), "tf_detector", read, "production"))
			reread, err := c.GetCheckRule(t.Context(), "tf_detector", "production")
			require.NoError(t, err)
			assert.Equal(t, read, reread)
			assert.Equal(t, 2, puts)
			assert.NotContains(t, reread, "dash0.com/volume-floor")
		})
	}
}
