# Configuration

The Dash0 Terraform Provider is configured through the standard `provider` block plus a set of environment variables.
Credentials can also be sourced from a [Dash0 CLI](https://dash0.com/docs/dash0/miscellaneous/tooling/dash0-cli/about) profile.

## Provider block schema

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `url` | string | Conditional | Base URL of the Dash0 API (for example, `https://api.us-west-2.aws.dash0.com`). Find yours on the [API endpoint settings page](https://app.dash0.com/goto/settings/endpoints?endpoint_type=api_http). Required unless `DASH0_API_URL` is set or a CLI profile supplies it. |
| `auth_token` | string (sensitive) | Conditional | Dash0 auth token. Must start with `auth_` (static token) or `dash0_at_` (OAuth access token). Required unless `DASH0_AUTH_TOKEN` is set or a CLI profile supplies it. |
| `otlp_url` | string | Optional | Base URL of the Dash0 OTLP/HTTP ingress endpoint (for example, `https://ingress.us-west-2.aws.dash0.com`). Find yours on the [OTLP endpoint settings page](https://app.dash0.com/goto/settings/endpoints?endpoint_type=otlp_http). A different host from `url`. Only the `dash0_log_event` and `dash0_deployment_event` actions need it; every resource works without it. Signal-specific paths such as `/v1/logs` are appended automatically and must not be included. |
| `profile` | string | Optional | Name of a Dash0 CLI profile whose credentials should be used. Only consulted when neither environment variables nor the `url`/`auth_token`/`otlp_url` attributes are set. |
| `max_retries` | number | Optional | Maximum number of retries for failed API requests. Range: `0`–`5`. Default: `3`. |

## Environment variables

Environment variables take precedence over the equivalent `provider` block attributes.

| Variable | Required | Description | Default |
|----------|----------|-------------|---------|
| `DASH0_API_URL` | Yes¹ | Base URL of the Dash0 API (for example, `https://api.us-west-2.aws.dash0.com`). Find yours on the [API endpoint settings page](https://app.dash0.com/goto/settings/endpoints?endpoint_type=api_http). Overrides the `url` provider attribute. | — |
| `DASH0_URL` | No | Deprecated alias for `DASH0_API_URL`. Used only when `DASH0_API_URL` is not set. | — |
| `DASH0_AUTH_TOKEN` | Yes¹ | The API auth token for Dash0. Must start with `auth_` or `dash0_at_`. Overrides the `auth_token` provider attribute. | — |
| `DASH0_OTLP_URL` | No | Base URL of the Dash0 OTLP/HTTP ingress endpoint. Find yours on the [OTLP endpoint settings page](https://app.dash0.com/goto/settings/endpoints?endpoint_type=otlp_http). Overrides the `otlp_url` provider attribute. Only needed by the log-event actions. | — |
| `DASH0_CONFIG_DIR` | No | Directory containing the Dash0 CLI configuration files (`activeProfile`, `profiles.json`). Used when loading credentials from a CLI profile. | `~/.dash0` |
| `DASH0_MAX_RETRIES` | No | Maximum number of retries for failed API requests (0–5). Overrides the `max_retries` provider attribute. | `3` |

¹ Required unless credentials are supplied through the `provider` block or a Dash0 CLI profile.

## Credential resolution order

The provider resolves credentials in this order and stops at the first source that supplies them:

1. The `DASH0_API_URL` and `DASH0_AUTH_TOKEN` environment variables.
   `DASH0_URL` is accepted as a deprecated fallback when `DASH0_API_URL` is not set.
2. The `url` and `auth_token` attributes on the `provider` block.
3. A named Dash0 CLI profile — the one named by the `profile` attribute, or the active profile in `~/.dash0/` when `profile` is unset.

The OTLP endpoint follows the same order (`DASH0_OTLP_URL`, then `otlp_url`, then the profile's `otlpUrl`) but is resolved independently of the credentials.
It is not part of the "credentials are complete" test, so a configuration that supplies the API URL and auth token but no OTLP endpoint is valid — only the log-event actions will fail, and they say so.

The Dash0 credentials can be found under [Dash0's settings screens](https://app.dash0.com/goto/settings/auth-tokens).

## Option 1: Environment variables (recommended)

```sh
# see https://app.dash0.com/goto/settings/endpoints?endpoint_type=api_http for the exact URL
export DASH0_API_URL="https://api.xxx.dash0.com"
export DASH0_AUTH_TOKEN="auth_xxxx"
export DASH0_MAX_RETRIES=3  # optional; default 3, max 5

# only needed for the dash0_log_event and dash0_deployment_event actions
# see https://app.dash0.com/goto/settings/endpoints?endpoint_type=otlp_http for the exact URL
export DASH0_OTLP_URL="https://ingress.xxx.dash0.com"
```

With those set, the `provider` block only needs the version pin:

```terraform
terraform {
  required_providers {
    dash0 = {
      source  = "dash0hq/dash0"
      version = "~> 1.6"
    }
  }
}

provider "dash0" {}
```

## Option 2: Provider-block attributes

```terraform
terraform {
  required_providers {
    dash0 = {
      source  = "dash0hq/dash0"
      version = "~> 1.6"
    }
  }
}

provider "dash0" {
  url        = "https://api.xxx.dash0.com"
  auth_token = "auth_xxxx"
}
```

Environment variables take precedence over provider-block attributes when both are set.

## Option 3: Dash0 CLI profile

When the [Dash0 CLI](https://dash0.com/docs/dash0/miscellaneous/tooling/dash0-cli/about) is installed and configured, the provider can load credentials from one of its profiles.
Set the `profile` attribute to pick a named profile, or omit it to fall back to the active profile.

```terraform
provider "dash0" {
  profile = "production"
}
```

The CLI configuration directory defaults to `~/.dash0`; set `DASH0_CONFIG_DIR` to point at a different location (useful for CI runners or sandboxed environments).

### OAuth-enabled profiles

Profiles authenticated via `dash0 auth login` (OAuth) are supported for every resource and data source.
The provider transparently refreshes the access token when it is close to expiry.
If the refresh token itself has expired or been revoked, the provider emits an error asking you to re-authenticate:

```
Error: OAuth re-authentication required

The OAuth session for your dash0 CLI profile has expired.
Run `dash0 auth login` to re-authenticate, then re-run your Terraform command.
```

~> **Note:** OAuth support was added in version [v1.14.0](https://github.com/dash0hq/terraform-provider-dash0/releases/tag/v1.14.0). Earlier versions reject OAuth-enabled profiles with an `Invalid Dash0 Auth Token` error because they require auth tokens to start with the `auth_` prefix — OAuth access tokens use `dash0_at_` instead, so upgrade the provider if you see this error.

~> **Note:** The `dash0_log_event` and `dash0_deployment_event` actions require a static token (`auth_` prefix, from [Dash0 Settings > Auth Tokens](https://app.dash0.com/goto/settings/auth-tokens)) — supplied via `auth_token`, `DASH0_AUTH_TOKEN`, or a non-OAuth profile. The Dash0 OTLP/HTTP ingress endpoint those actions send to does not accept OAuth access tokens, even though the Dash0 API does, so an OAuth-enabled profile fails those actions with an actionable error.
