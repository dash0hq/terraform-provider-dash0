package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

func TestNewDash0Client_RecordingRule(t *testing.T) {
	// Verify client creation works (recording rule methods are available on the client)
	c, err := NewDash0Client("https://api.example.com", dash0.StaticAuthTokenProvider("auth_test-token"), "test", 3)
	require.NoError(t, err)
	assert.NotNil(t, c)
}
