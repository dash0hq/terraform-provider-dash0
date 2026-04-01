package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnmarshalView(t *testing.T) {
	json := `{"kind":"View","metadata":{"name":"test"},"spec":{"display":{"name":"Test"}}}`
	def, err := unmarshalView(json)
	require.NoError(t, err)
	assert.NotNil(t, def)
}

func TestUnmarshalView_Invalid(t *testing.T) {
	_, err := unmarshalView("not json")
	assert.Error(t, err)
}
