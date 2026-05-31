terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0"
    }
  }
}

provider "aws" {
  region = var.aws_region
}

resource "aws_cloudformation_stack" "dash0_integration" {
  name         = "dash0-integration"
  template_url = "https://public-integrations-production.eu-west-1.aws.dash0.com.s3.eu-west-1.amazonaws.com/dash0-customer-integration-cloudformation-v2.yaml"

  parameters = {
    ApiKey                       = var.api_key
    Dataset                      = var.dataset
    ResourcesInstrumentation     = var.resources_instrumentation
    CollectLambdaLifecycleEvents = var.collect_lambda_lifecycle_events
    TechnicalId                  = var.technical_id
    Regions                      = join(",", var.regions)
    Dash0RegionalEndpoint        = var.dash0_regional_endpoint
  }

  # CAPABILITY_NAMED_IAM: the template creates named IAM roles.
  # CAPABILITY_AUTO_EXPAND: the template contains a nested AWS::CloudFormation::StackSet
  # that deploys the per-region Dash0 resources (Lambda metrics streaming and, optionally,
  # Lambda lifecycle event forwarding).
  capabilities = ["CAPABILITY_NAMED_IAM", "CAPABILITY_AUTO_EXPAND"]
}

output "stack_id" {
  value = aws_cloudformation_stack.dash0_integration.id
}

output "stack_outputs" {
  value = aws_cloudformation_stack.dash0_integration.outputs
}
