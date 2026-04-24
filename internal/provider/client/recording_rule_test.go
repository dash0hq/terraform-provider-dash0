package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDash0Client_RecordingRule(t *testing.T) {
	// Verify client creation works (recording rule methods are available on the client)
	c, err := NewDash0Client("https://api.example.com", "auth_test-token", "test")
	require.NoError(t, err)
	assert.NotNil(t, c)
}
