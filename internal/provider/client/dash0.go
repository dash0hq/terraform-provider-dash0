package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"
	"golang.org/x/sync/semaphore"
)

// dash0Client is the client implementation for interacting with the Dash0 API.
type dash0Client struct {
	url         string
	authToken   string
	client      *http.Client
	semaphore   *semaphore.Weighted
	maxParallel int64
}

// NewDash0Client creates a new Dash0 API client.
func NewDash0Client(url, authToken string) *dash0Client {
	maxParallel := int64(10) // Maximum number of parallel HTTP requests
	return &dash0Client{
		url:       url,
		authToken: authToken,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		semaphore:   semaphore.NewWeighted(maxParallel),
		maxParallel: maxParallel,
	}
}

// doRequest performs an HTTP request against the Dash0 API.
func (c *dash0Client) doRequest(ctx context.Context, method, path string, body string) ([]byte, error) {
	// Acquire semaphore to limit concurrent requests
	if err := c.semaphore.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf("failed to acquire semaphore: %w", err)
	}
	// Release the semaphore when done
	defer c.semaphore.Release(1)

	tflog.Debug(ctx, fmt.Sprintf("Acquired semaphore for request to %s %s", method, path))

	var reqBody io.Reader
	if body != "" {
		reqBody = bytes.NewBuffer([]byte(body))
	}

	url := fmt.Sprintf("%s%s", c.url, path)
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Dash0 Terraform Provider")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.authToken))

	tflog.Debug(ctx, fmt.Sprintf("Making request to Dash0 API: %s %s", method, path))

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}
