---
page_title: "Guide: AWS Integration via CloudFormation"
subcategory: ""
description: |-
  Deploy the Dash0 AWS integration using the official CloudFormation template, managed via Terraform.
---

# AWS Integration via CloudFormation

This guide shows how to deploy the Dash0 AWS integration using Terraform's [`aws_cloudformation_stack`](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudformation_stack) resource.

~> **Note:** This is not a native Dash0 provider resource. It uses the AWS provider to deploy a CloudFormation stack with the official Dash0 integration template.

The integration provisions the required IAM roles and configuration for collecting telemetry from your AWS account.

## Prerequisites

- An active [Dash0](https://dash0.com) organization.
- A Dash0 **API key** (auth token), created under **Settings > Auth Tokens** in the Dash0 UI.
- Your Dash0 organization **technical ID**, found under **Settings > Organization** in the Dash0 UI.
- An AWS account with permissions to create CloudFormation stacks and IAM roles.
- Terraform >= 1.0 and the [AWS provider](https://registry.terraform.io/providers/hashicorp/aws/latest) >= 5.0.

## Example Usage

A complete, runnable example is available in [`examples/guides/aws_cloudformation_integration/`](https://github.com/dash0hq/terraform-provider-dash0/tree/main/examples/guides/aws_cloudformation_integration).

The core resource definition is:

```terraform
resource "aws_cloudformation_stack" "dash0_integration" {
  name         = "dash0-integration"
  template_url = "https://public-integrations-production.eu-west-1.aws.dash0.com.s3.eu-west-1.amazonaws.com/dash0-customer-integration-cloudformation.json"

  parameters = {
    ApiKey                   = var.api_key
    Dataset                  = var.dataset
    ResourcesInstrumentation = var.resources_instrumentation
    TechnicalId              = var.technical_id
  }

  capabilities = ["CAPABILITY_NAMED_IAM"]
}
```

The `CAPABILITY_NAMED_IAM` capability is required because the template creates named IAM roles for the Dash0 integration.

## CloudFormation Parameters

| Parameter | Description | Required | Default |
|-----------|-------------|----------|---------|
| `ApiKey` | Dash0 API key (auth token) for the integration. | Yes | -- |
| `Dataset` | The Dash0 dataset to send telemetry data to. | No | `default` |
| `ResourcesInstrumentation` | Enable instrumentation of AWS Lambda and other resources (`"true"` or `"false"`). | No | `"false"` |
| `TechnicalId` | The Dash0 organization technical ID. | Yes | -- |

## Getting Started

1. Copy the [example directory](https://github.com/dash0hq/terraform-provider-dash0/tree/main/examples/guides/aws_cloudformation_integration) into your project, or use the resource definition above in your own configuration.

2. Create a `terraform.tfvars` file with your values (do **not** commit this file):

   ```hcl
   api_key      = "your-dash0-auth-token"
   technical_id = "your-organization-technical-id"
   ```

3. Initialize and apply:

   ```shell
   terraform init
   terraform plan
   terraform apply
   ```

4. To tear down the integration:

   ```shell
   terraform destroy
   ```

## Notes

- The `api_key` variable is marked as `sensitive` so Terraform will not display its value in plan or apply output.
- The CloudFormation stack is region-specific. Deploy it in the region where your AWS workloads run, or in the region closest to your Dash0 environment.
