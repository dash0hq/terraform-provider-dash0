# Dash0 AWS Integration (Terraform module)

Turnkey wrapper for the `dash0_aws_integration` resource: creates the required AWS IAM roles and policies, then registers the integration with the Dash0 API — in one `module` block.

This is the recommended path for most users. If you need custom IAM policies, lifecycle rules, or want to reuse roles created elsewhere, use the `dash0_aws_integration` resource directly instead.

## Usage

```hcl
terraform {
  required_providers {
    aws   = { source = "hashicorp/aws" }
    dash0 = { source = "dash0hq/dash0" }
  }
}

provider "aws" {
  region = "us-east-1"

  # Optional: default_tags cascade into the module when you pass the provider below.
  default_tags {
    tags = {
      Team      = "platform"
      ManagedBy = "terraform"
    }
  }
}

provider "dash0" {
  url        = "https://api.dash0.com"
  auth_token = var.dash0_token
}

module "dash0_integration" {
  source = "git::https://github.com/dash0hq/terraform-provider-dash0.git//modules/aws_integration?ref=v0.2.0"

  external_id            = var.dash0_org_id
  dataset                = "default"
  enable_instrumentation = true

  # Cascade provider-level default_tags into the module (recommended).
  providers = { aws = aws }
}
```

`source` accepts a branch, tag, or commit SHA via `?ref=...`. Pin to a release tag in production.

## Tagging

Two supported modes, combinable:

- **Provider pass-through (recommended):** pass `providers = { aws = aws }` to the module call and set `default_tags` on the `aws` provider. AWS merges those tags into every resource the module creates.
- **Explicit `tags` variable:** pass a `tags = {...}` map. Useful when you don't want to pass the provider, or want module-specific tags in addition to `default_tags`.

## What the module creates

| Resource | Name | Purpose |
|----------|------|---------|
| `aws_iam_role.readonly` | `<prefix>-read-only` | Dash0-assumable role for resource discovery |
| `aws_iam_role_policy_attachment.readonly_view_only_access` | — | Attaches AWS-managed `ViewOnlyAccess` |
| `aws_iam_role_policy.readonly_dash0` | `Dash0ReadOnly` | Inline policy covering Resource Explorer, Tags, Lambda, EKS, AppSync, X-Ray |
| `aws_iam_role.instrumentation` (optional) | `<prefix>-instrumentation` | Dash0-assumable role for Lambda auto-instrumentation |
| `aws_iam_policy.instrumentation` (optional) | `<prefix>-lambda-instrumentation` | Managed policy with Lambda + EC2 permissions |
| `aws_iam_role_policy_attachment.instrumentation` (optional) | — | Attaches the policy above to the instrumentation role |
| `dash0_aws_integration.this` | — | Registers the integration with the Dash0 API |

`<prefix>` is `iam_role_name_prefix` (default `dash0`). The policy name is prefix-scoped so multiple Dash0 integrations can coexist in the same AWS account.

## Inputs

| Name | Type | Default | Description |
|------|------|---------|-------------|
| `external_id` | string | — | Dash0 organization technical ID. Required. |
| `dataset` | string | `"default"` | Dash0 dataset slug. |
| `iam_role_name_prefix` | string | `"dash0"` | Prefix applied to the IAM role names. |
| `enable_instrumentation` | bool | `false` | Create the Lambda instrumentation role. |
| `dash0_aws_account_id` | string | `"115813213817"` | Dash0-owned AWS account; only override if directed by Dash0. |
| `tags` | map(string) | `{}` | Tags applied to IAM roles and the instrumentation policy (merges with provider `default_tags`). |

## Outputs

| Name | Description |
|------|-------------|
| `aws_account_id` | AWS account ID where the IAM roles were created. |
| `read_only_role_arn` | ARN of the read-only role. |
| `instrumentation_role_arn` | ARN of the instrumentation role, or `null` when disabled. |
| `integration_id` | Composite ID of the `dash0_aws_integration` resource. |

## Requirements

- Terraform ≥ 1.0
- `hashicorp/aws` ≥ 5.0
- `dash0hq/dash0` ≥ 0.1.0
