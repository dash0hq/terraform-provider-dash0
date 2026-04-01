package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDash0Client(t *testing.T) {
	c, err := NewDash0Client("https://api.example.com", "auth_test-token", "test")
	require.NoError(t, err)
	assert.NotNil(t, c)
}

func TestNewDash0Client_InvalidToken(t *testing.T) {
	_, err := NewDash0Client("https://api.example.com", "invalid-token", "test")
	assert.Error(t, err)
}

