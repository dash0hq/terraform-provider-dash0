package client

import (
	"fmt"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

// dash0Client wraps the dash0-api-client-go library client.
type dash0Client struct {
	inner dash0.Client
}

// NewDash0Client creates a new Dash0 API client backed by the shared library.
func NewDash0Client(url, authToken, version string) (*dash0Client, error) {
	c, err := dash0.NewClient(
		dash0.WithApiUrl(url),
		dash0.WithAuthToken(authToken),
		dash0.WithUserAgent(fmt.Sprintf("Dash0 Terraform Provider/%s", version)),
	)
	if err != nil {
		return nil, err
	}
	return &dash0Client{inner: c}, nil
}
