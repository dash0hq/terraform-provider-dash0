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
  template_url = "https://public-integrations-production.eu-west-1.aws.dash0.com.s3.eu-west-1.amazonaws.com/dash0-customer-integration-cloudformation.json"

  parameters = {
    ApiKey                   = var.api_key
    Dataset                  = var.dataset
    ResourcesInstrumentation = var.resources_instrumentation
    TechnicalId              = var.technical_id
  }

  capabilities = ["CAPABILITY_NAMED_IAM"]
}

output "stack_id" {
  value = aws_cloudformation_stack.dash0_integration.id
}

output "stack_outputs" {
  value = aws_cloudformation_stack.dash0_integration.outputs
}
