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
	// otlpURL is the configured Dash0 OTLP/HTTP ingress URL, or the empty
	// string when the provider was configured without one. It is retained so
	// SendLogEvent can distinguish "not configured" from a transport failure
	// and produce an actionable diagnostic.
	otlpURL string
	// isOAuthToken records whether authToken is an OAuth access token
	// (`dash0_at_` prefix) rather than a static token (`auth_` prefix). The
	// Dash0 OTLP/HTTP ingress endpoint does not currently accept OAuth access
	// tokens, even though the REST API does; SendLogEvent uses this to fail
	// fast with an actionable diagnostic instead of a bare 401 from the
	// server. Resolved by the caller (the provider already knows whether the
	// token came from an OAuth-enabled CLI profile) rather than re-derived
	// here from the token's prefix, to avoid two sources of truth.
	isOAuthToken bool
	// version is the provider version. It is used as the OpenTelemetry
	// instrumentation scope version on emitted telemetry.
	version string
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
//
// isOAuthToken is resolved by the caller (the provider already knows whether
// the token came from an OAuth-enabled CLI profile) rather than derived here:
// there is no longer a static token string to inspect the prefix of once
// authentication goes through a refreshing provider.
//
// otlpURL is optional: when empty, SendLogEvent returns an error explaining
// how to configure it, and all REST API operations are unaffected.
func NewDash0Client(url string, authTokenProvider dash0.AuthTokenProvider, isOAuthToken bool, version string, maxRetries int, otlpURL string) (*dash0Client, error) {
	clientOpts := []dash0.ClientOption{
		dash0.WithApiUrl(url),
		dash0.WithAuthTokenProvider(authTokenProvider),
		dash0.WithUserAgent(fmt.Sprintf("Dash0 Terraform Provider/%s", version)),
		dash0.WithMaxRetries(maxRetries),
	}
	if otlpURL != "" {
		clientOpts = append(clientOpts, dash0.WithOtlpEndpoint(dash0.OtlpEncodingJson, otlpURL))
	}

	c, err := dash0.NewClient(clientOpts...)
	if err != nil {
		return nil, err
	}
	return &dash0Client{
		inner:        c,
		apiURL:       url,
		otlpURL:      otlpURL,
		isOAuthToken: isOAuthToken,
		version:      version,
	}, nil
}
