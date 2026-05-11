---
page_title: "Guide: AWS Integration via CloudFormation"
subcategory: ""
description: |-
  Deploy the Dash0 AWS integration using the official CloudFormation template, managed via Terraform.
---

# AWS Integration via CloudFormation

This guide shows how to deploy the Dash0 AWS integration using Terraform's [`aws_cloudformation_stack`](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudformation_stack) resource.

~> **Note:** This is not a native Dash0 provider resource. It uses the AWS provider to deploy a CloudFormation stack with the official Dash0 integration template.

The integration provisions the required IAM roles and configuration for collecting telemetry from your AWS account. The v2 template additionally provisions, via a CloudFormation StackSet, a Kinesis Firehose delivery stream, a CloudWatch metric stream and an S3 backup bucket per region for the `AWS/Lambda` namespace.

## Prerequisites

- An active [Dash0](https://dash0.com) organization.
- A Dash0 **API key** (auth token), created under **Settings > Auth Tokens** in the Dash0 UI.
- Your Dash0 organization **technical ID**, found under **Settings > Organization** in the Dash0 UI.
- Your Dash0 **CloudWatch Metrics Firehose ingress URL**, found under **Settings > Endpoints > AWS CloudWatch Metrics** in the Dash0 UI (for example `https://ingress.eu-west-1.aws.dash0.com/firehose/cwmetrics`).
- An AWS account with permissions to create CloudFormation stacks, StackSets, and IAM roles.
- Terraform >= 1.0 and the [AWS provider](https://registry.terraform.io/providers/hashicorp/aws/latest) >= 5.0.

## Example Usage

A complete, runnable example is available in [`examples/guides/aws_cloudformation_integration/`](https://github.com/dash0hq/terraform-provider-dash0/tree/main/examples/guides/aws_cloudformation_integration).

The core resource definition is:

```terraform
resource "aws_cloudformation_stack" "dash0_integration" {
  name         = "dash0-integration"
  template_url = "https://public-integrations-production.eu-west-1.aws.dash0.com.s3.eu-west-1.amazonaws.com/dash0-customer-integration-cloudformation-v2.yaml"

  parameters = {
    ApiKey                   = var.api_key
    Dataset                  = var.dataset
    ResourcesInstrumentation = var.resources_instrumentation
    TechnicalId              = var.technical_id
    Regions                  = join(",", var.regions)
    Dash0IngressUrl          = var.dash0_ingress_url
  }

  capabilities = ["CAPABILITY_NAMED_IAM", "CAPABILITY_AUTO_EXPAND"]
}
```

The `CAPABILITY_NAMED_IAM` capability is required because the template creates named IAM roles for the Dash0 integration. The `CAPABILITY_AUTO_EXPAND` capability is required because the v2 template includes a nested `AWS::CloudFormation::StackSet` that deploys the per-region Lambda metrics streaming resources.

CloudFormation expects `Regions` to be a `CommaDelimitedList`, so the Terraform list variable is joined into a comma-separated string before being passed to the stack.

## CloudFormation Parameters

| Parameter | Description | Required | Default |
|-----------|-------------|----------|---------|
| `ApiKey` | Dash0 API key (auth token) for the integration. | Yes | -- |
| `Dataset` | The Dash0 dataset to send telemetry data to. | No | `default` |
| `ResourcesInstrumentation` | Enable instrumentation of AWS Lambda and other resources (`"true"` or `"false"`). | No | `"true"` |
| `TechnicalId` | The Dash0 organization technical ID. | Yes | -- |
| `Regions` | Comma-separated list of AWS regions in which to enable `AWS/Lambda` metrics streaming (for example `eu-west-1,us-east-1`). | Yes | -- |
| `Dash0IngressUrl` | Regional Firehose ingress endpoint for your Dash0 organization. Find it under **Settings > Endpoints > AWS CloudWatch Metrics** in the Dash0 UI (for example `https://ingress.eu-west-1.aws.dash0.com/firehose/cwmetrics`). | Yes | -- |

## Getting Started

1. Copy the [example directory](https://github.com/dash0hq/terraform-provider-dash0/tree/main/examples/guides/aws_cloudformation_integration) into your project, or use the resource definition above in your own configuration.

2. Create a `terraform.tfvars` file with your values (do **not** commit this file):

   ```hcl
   api_key           = "your-dash0-auth-token"
   technical_id      = "your-organization-technical-id"
   dash0_ingress_url = "https://ingress.eu-west-1.aws.dash0.com/firehose/cwmetrics"
   regions           = ["eu-west-1"]
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
- The CloudFormation stack is region-specific. Deploy it in the region where your AWS workloads run, or in the region closest to your Dash0 environment. The `Regions` parameter controls which regions the nested StackSet provisions Lambda metrics streaming into; it is independent from the region the parent stack itself is deployed in.
- The S3 backup bucket created by the StackSet is retained on `terraform destroy` by design. Delete it manually if you want to fully reclaim the bucket name before re-onboarding.
