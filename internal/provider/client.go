package provider

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// dash0Client is the client implementation for interacting with the Dash0 API.
type dash0Client struct {
	url       string
	authToken string
	client    *http.Client
}

// newDash0Client creates a new Dash0 API client.
func newDash0Client(url, authToken string) *dash0Client {
	return &dash0Client{
		url:       url,
		authToken: authToken,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// doRequest performs an HTTP request against the Dash0 API.
func (c *dash0Client) doRequest(ctx context.Context, method, path string, body string) ([]byte, error) {
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
