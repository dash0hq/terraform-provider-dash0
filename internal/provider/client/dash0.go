package client

import (
	"fmt"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

// dash0Client wraps the dash0-api-client-go library client.
type dash0Client struct {
	inner dash0.Client
	// apiURL is the configured Dash0 API base URL. It is retained so the
	// provider can derive the Dash0 web app base URL for dashboard deep links.
	apiURL string
}

// NewDash0Client creates a new Dash0 API client backed by the shared library.
//
// Authentication goes through an [dash0.AuthTokenProvider] rather than a fixed
// token so that an OAuth access token is refreshed for the whole duration of a
// Terraform run. An access token is valid for 15 minutes, and a plan or apply
// over a large configuration routinely takes longer than that; a token captured
// once at Configure time would start failing with 401 partway through.
// Credentials that cannot expire — a static `auth_*` token from the environment
// or the provider block — are passed as a static provider.
func NewDash0Client(url string, authTokenProvider dash0.AuthTokenProvider, version string, maxRetries int) (*dash0Client, error) {
	c, err := dash0.NewClient(
		dash0.WithApiUrl(url),
		dash0.WithAuthTokenProvider(authTokenProvider),
		dash0.WithUserAgent(fmt.Sprintf("Dash0 Terraform Provider/%s", version)),
		dash0.WithMaxRetries(maxRetries),
	)
	if err != nil {
		return nil, err
	}
	return &dash0Client{inner: c, apiURL: url}, nil
}
