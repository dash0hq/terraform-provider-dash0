// SPDX-FileCopyrightText: Copyright 2023-2026 Dash0 Inc.

package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDash0Client_RecordingRuleGroup(t *testing.T) {
	// Verify client creation works (recording rule group methods are available on the client)
	c, err := NewDash0Client("https://api.example.com", "auth_test-token", "test")
	require.NoError(t, err)
	assert.NotNil(t, c)
}
