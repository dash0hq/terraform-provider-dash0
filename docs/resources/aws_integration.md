---
page_title: "dash0_aws_integration Resource - terraform-provider-dash0"
subcategory: ""
description: |-
  Manages a Dash0 AWS integration, including IAM roles for resource discovery and monitoring.
---

# dash0_aws_integration (Resource)

Manages a Dash0 AWS integration. Creates IAM roles for resource discovery and monitoring, and registers the integration with the Dash0 API. Optionally creates an instrumentation role for Lambda auto-instrumentation.

This resource creates the following AWS IAM roles:
- **Read-only role** (`<prefix>-read-only`): Allows Dash0 to discover and monitor your AWS resources via Resource Explorer, Tags, Lambda, EKS, AppSync, and X-Ray APIs.
- **Instrumentation role** (`<prefix>-instrumentation`, optional): Allows Dash0 to auto-instrument Lambda functions.

## Example Usage

### Basic monitoring integration

```terraform
resource "dash0_aws_integration" "monitoring" {
  dataset     = "default"
  external_id = "your-dash0-org-technical-id"
}
```

### Full integration with Lambda auto-instrumentation

```terraform
resource "dash0_aws_integration" "full" {
  dataset     = "default"
  external_id = "your-dash0-org-technical-id"

  enable_resources_instrumentation = true
  iam_role_name_prefix             = "dash0"

  tags = {
    Environment = "production"
    ManagedBy   = "terraform"
  }
}
```

## Argument Reference

### Required

- `dataset` (String) The Dash0 dataset slug to associate with this integration.
- `external_id` (String) The Dash0 organization technical ID, used as the STS AssumeRole external ID. Changing this forces a new resource.

### Optional

- `iam_role_name_prefix` (String) Prefix for the IAM role names. Defaults to `dash0`. Changing this forces a new resource.
- `enable_resources_instrumentation` (Boolean) Whether to create an additional IAM role for resources instrumentation (e.g., Lambda auto-instrumentation). Defaults to `false`.
- `dash0_aws_account_id` (String) The Dash0 AWS account ID that will assume the IAM roles. Defaults to `115813213817`. Changing this forces a new resource.
- `tags` (Map of String) Tags to apply to all IAM resources created by this resource.
- `aws_region` (String) AWS region. If omitted, uses the default AWS SDK credential chain. Changing this forces a new resource.
- `aws_profile` (String) AWS shared config profile name. Changing this forces a new resource.
- `aws_access_key` (String, Sensitive) AWS access key ID. Changing this forces a new resource.
- `aws_secret_key` (String, Sensitive) AWS secret access key. Changing this forces a new resource.

## Attribute Reference

- `id` (String) Composite identifier in the format `{aws_account_id}-{external_id}`.
- `read_only_role_arn` (String) The ARN of the Dash0 read-only IAM role.
- `instrumentation_role_arn` (String) The ARN of the Dash0 resources instrumentation IAM role (empty if not enabled).
- `aws_account_id` (String) The AWS account ID where the integration was created.

## Import

Import is supported using the following format:

```shell
# Format: "dataset,external_id[,iam_role_name_prefix]"
terraform import dash0_aws_integration.example "default,your-dash0-org-technical-id"

# With custom prefix
terraform import dash0_aws_integration.example "default,your-dash0-org-technical-id,my-prefix"
```

After import, run `terraform plan` to verify the state matches the actual AWS resources. The read operation will verify IAM roles exist and update the state accordingly.
