---
page_title: "Dash0 AWS Integration via CloudFormation"
subcategory: ""
description: |-
  Deploy the Dash0 AWS integration using the official CloudFormation template hosted on S3, managed via Terraform.
---

# Dash0 AWS Integration via Terraform

This guide shows how to deploy the Dash0 AWS integration using Terraform's [`aws_cloudformation_stack`](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudformation_stack) resource. The integration uses the official Dash0 CloudFormation template hosted on S3, which provisions the required IAM roles and configuration for collecting telemetry from your AWS account.

## Prerequisites

- An active [Dash0](https://dash0.com) organization.
- A Dash0 API key (auth token). You can create one under **Settings > Auth Tokens** in the Dash0 UI.
- Your Dash0 organization **technical ID**, found under **Settings > Organization** in the Dash0 UI.
- An AWS account with permissions to create CloudFormation stacks and IAM roles.
- Terraform >= 1.0 and the [AWS provider](https://registry.terraform.io/providers/hashicorp/aws/latest) >= 5.0.

## Example Usage

```terraform
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0"
    }
  }
}

provider "aws" {
  region = "eu-west-1"
}

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

## CloudFormation Parameters

| Parameter | Description | Required | Default |
|-----------|-------------|----------|---------|
| `ApiKey` | Dash0 API key (auth token) for the integration. | Yes | — |
| `Dataset` | The Dash0 dataset to send telemetry data to. | No | `default` |
| `ResourcesInstrumentation` | Enable instrumentation of AWS Lambda and other resources. Set to `"true"` or `"false"`. | No | `"false"` |
| `TechnicalId` | The Dash0 organization technical ID. | Yes | — |

## Variables

Define these variables in a `variables.tf` file:

```terraform
variable "aws_region" {
  type        = string
  description = "The AWS region to deploy the CloudFormation stack in."
  default     = "eu-west-1"
}

variable "api_key" {
  type        = string
  sensitive   = true
  description = "The Dash0 API key (auth token) for the integration."
}

variable "dataset" {
  type        = string
  description = "The Dash0 dataset to send data to."
  default     = "default"
}

variable "resources_instrumentation" {
  type        = string
  description = "Whether to enable Lambda and other AWS resource instrumentation."
  default     = "false"
}

variable "technical_id" {
  type        = string
  description = "The Dash0 organization technical ID."
}
```

## Outputs

The stack exposes these outputs:

```terraform
output "stack_id" {
  value = aws_cloudformation_stack.dash0_integration.id
}

output "stack_outputs" {
  value = aws_cloudformation_stack.dash0_integration.outputs
}
```

`stack_outputs` contains the values returned by the CloudFormation template, such as the IAM role ARNs created for the integration.

## Usage

1. Copy the example files or create a new Terraform configuration using the example above.

2. Create a `terraform.tfvars` file with your values (do **not** commit this file):

   ```hcl
   api_key      = "your-dash0-auth-token"
   dataset      = "default"
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

- The CloudFormation template requires the `CAPABILITY_NAMED_IAM` capability because it creates named IAM roles for the Dash0 integration.
- The `api_key` variable is marked as `sensitive` so Terraform will not display its value in plan or apply output.
- The CloudFormation stack is region-specific. Deploy it in the region where your AWS workloads run, or in the region closest to your Dash0 environment.
