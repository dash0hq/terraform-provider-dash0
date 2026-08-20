package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

func TestNewDash0Client(t *testing.T) {
	c, err := NewDash0Client("https://api.example.com", dash0.StaticAuthTokenProvider("auth_test-token"), "test", 3)
	require.NoError(t, err)
	assert.NotNil(t, c)
}

// TestNewDash0Client_InvalidToken documents that a malformed token is no longer
// rejected when the client is constructed.
//
// Authentication is resolved per request now, so the token's shape is checked
// when a request is about to go out rather than up front -- a provider may not
// have a token to hand at construction time. Users still get a fast, actionable
// error: the provider's Configure validates the prefix before reaching this
// constructor. See TestDash0Provider_Configure_InvalidAuthToken.
func TestNewDash0Client_InvalidToken(t *testing.T) {
	c, err := NewDash0Client("https://api.example.com", dash0.StaticAuthTokenProvider("invalid-token"), "test", 3)
	require.NoError(t, err)
	assert.NotNil(t, c)
}
