package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnmarshalSyntheticCheck(t *testing.T) {
	json := `{"kind":"SyntheticCheck","metadata":{"name":"test"},"spec":{"plugin":{"kind":"http"}}}`
	def, err := unmarshalSyntheticCheck(json)
	require.NoError(t, err)
	assert.NotNil(t, def)
}

func TestUnmarshalSyntheticCheck_Invalid(t *testing.T) {
	_, err := unmarshalSyntheticCheck("not json")
	assert.Error(t, err)
}
