# Terraform Provider for Dash0

The Dash0 provider allows Terraform to manage resources on [Dash0](https://dash0.com) observability platform. This provider can be used to define dashboards in Perses dashboard format.

## Example Usage

```hcl
# Configure the Dash0 provider
provider "dash0" {
  # No configuration needed in the provider block
  # Authentication is handled via environment variables
  # DASH0_URL and DASH0_AUTH_TOKEN
}

# Create a dashboard by loading YAML from a file
resource "dash0_dashboard" "example" {
  name        = "example-dashboard"
  description = "Example dashboard created via Terraform"
  dataset     = "default"  # Optional, defaults to "default" if not specified

  # Load the dashboard definition from a local YAML file
  dashboard_yaml = file("${path.module}/dashboards/example-dashboard.yaml")
}

```

## Authentication

The Dash0 provider authenticates using environment variables:

```shell
export DASH0_AUTH_TOKEN="auth_xxxx"
export DASH0_URL="https://api.us-west-2.aws.dash0.com"  # Optional, defaults to https://api.us-west-2.aws.dash0.com
```

```hcl
provider "dash0" {}
```

## Resources

- `dash0_dashboard` - Manages a Dash0 Dashboard in Perses format

## Developing the Provider

### Requirements

- [Go](https://golang.org/doc/install) 1.21 or higher
- [Terraform](https://developer.hashicorp.com/terraform/downloads) 1.0.0 or higher

### Building

```shell
go build -o terraform-provider-dash0
```

### Installing

For local development and testing, you can install the provider to your local Terraform plugin directory:

```shell
mkdir -p ~/.terraform.d/plugins/registry.terraform.io/dash0/dash0/dev/$(go env GOOS)_$(go env GOARCH)/
cp terraform-provider-dash0 ~/.terraform.d/plugins/registry.terraform.io/dash0/dash0/dev/$(go env GOOS)_$(go env GOARCH)/
```

## License

This project is licensed under the [Apache License 2.0](LICENSE).
