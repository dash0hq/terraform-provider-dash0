package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"
	"golang.org/x/sync/semaphore"
)

// dash0Client is the client implementation for interacting with the Dash0 API.
type dash0Client struct {
	url         string
	authToken   string
	version     string
	client      *http.Client
	semaphore   *semaphore.Weighted
	maxParallel int64
}

// NewDash0Client creates a new Dash0 API client.
func NewDash0Client(url, authToken, version string) *dash0Client {
	maxParallel := int64(10) // Maximum number of parallel HTTP requests
	return &dash0Client{
		url:       url,
		authToken: authToken,
		version:   version,
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

	apiURL := fmt.Sprintf("%s%s", c.url, path)
	req, err := http.NewRequestWithContext(ctx, method, apiURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", fmt.Sprintf("Dash0 Terraform Provider/%s", c.version))
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.authToken))

	tflog.Debug(ctx, fmt.Sprintf("Making request to Dash0 API: %s %s", method, path))

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request: %w", err)
	}
	//nolint:errcheck
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

func (c *dash0Client) create(ctx context.Context, dataset string, apiPath string, jsonBody string, resourceName string) error {
	u, err := url.Parse(apiPath)
	if err != nil {
		return fmt.Errorf("error parsing API path: %w", err)
	}

	// Add dataset as a query parameter
	q := u.Query()
	q.Set("dataset", dataset)
	u.RawQuery = q.Encode()

	tflog.Debug(ctx, fmt.Sprintf("Creating %s with JSON payload: %s", resourceName, jsonBody))

	// Make the API request with JSON
	resp, err := c.doRequest(ctx, http.MethodPut, u.String(), jsonBody)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("%s created. Got API response: %s", resourceName, resp))
	return nil
}

func (c *dash0Client) update(ctx context.Context, origin string, dataset string, apiPath string, jsonBody string, resourceName string) error {
	u, err := url.Parse(apiPath)
	if err != nil {
		return fmt.Errorf("error parsing API path: %w", err)
	}

	// Add dataset as a query parameter
	q := u.Query()
	q.Set("dataset", dataset)
	u.RawQuery = q.Encode()

	tflog.Debug(ctx, fmt.Sprintf("Updating %s in %s with JSON payload: %s", resourceName, dataset, jsonBody))

	// Make the API request with JSON
	_, err = c.doRequest(ctx, http.MethodPut, u.String(), jsonBody)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("%s updated with origin: %s", resourceName, origin))
	return nil
}

func (c *dash0Client) get(ctx context.Context, origin string, dataset string, apiPath string, resourceName string) ([]byte, error) {
	u, err := url.Parse(apiPath)
	if err != nil {
		return nil, fmt.Errorf("error parsing API path: %w", err)
	}

	// Add dataset as a query parameter
	q := u.Query()
	q.Set("dataset", dataset)
	u.RawQuery = q.Encode()

	resp, err := c.doRequest(ctx, http.MethodGet, u.String(), "")
	if err != nil {
		return nil, err
	}

	tflog.Debug(ctx, fmt.Sprintf("%s retrieved with origin: %s", resourceName, origin))

	return resp, nil
}

func (c *dash0Client) delete(ctx context.Context, origin string, dataset string, apiPath string, resourceName string) error {
	// Build URL with dataset query parameter
	u, err := url.Parse(apiPath)
	if err != nil {
		return fmt.Errorf("error parsing API path: %w", err)
	}

	// Add dataset as a query parameter
	q := u.Query()
	q.Set("dataset", dataset)
	u.RawQuery = q.Encode()

	tflog.Debug(ctx, fmt.Sprintf("Deleting %s in dataset: %s", resourceName, dataset))

	// Make the API request
	_, err = c.doRequest(ctx, http.MethodDelete, u.String(), "")
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("%s deleted with origin: %s", resourceName, origin))

	return nil
}
