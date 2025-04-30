# Complete Dash0 Provider Example

This directory contains a complete example of using the Dash0 Terraform Provider to manage dashboards with YAML definitions stored in separate files.

## Dashboard YAML Files

Instead of embedding YAML directly in the Terraform configuration, this example loads the YAML from separate files:

- Dashboard definitions are stored in the `dashboards/` directory

This approach provides several benefits:

1. Better separation of concerns (Terraform manages resources, YAML files define the content)
2. YAML files can be edited independently by dashboard/alert designers
3. Dashboard and alert rule configurations can be more easily version-controlled
4. IDE support for syntax highlighting and validation of YAML files

## Usage

```bash
# Set your Dash0 environment variables
export DASH0_AUTH_TOKEN="auth_xxxx"
export DASH0_URL="https://api.us-west-2.aws.dash0.com"  # Optional, defaults to https://api.us-west-2.aws.dash0.com

# Initialize Terraform
terraform init

# Apply the configuration
terraform apply
```

## Adding New Dashboards

To add a new dashboard:

1. Create a new YAML file in the `dashboards/` directory
2. Add a new `dash0_dashboard` resource to the Terraform configuration that references the file
