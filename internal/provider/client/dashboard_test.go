package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnmarshalDashboard(t *testing.T) {
	json := `{"kind":"Dashboard","metadata":{"name":"test"},"spec":{"title":"Test"}}`
	def, err := unmarshalDashboard(json)
	require.NoError(t, err)
	assert.NotNil(t, def)
}

func TestUnmarshalDashboard_Invalid(t *testing.T) {
	_, err := unmarshalDashboard("not json")
	assert.Error(t, err)
}
