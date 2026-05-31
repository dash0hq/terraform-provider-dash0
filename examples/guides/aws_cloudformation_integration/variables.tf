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
  description = "Whether to enable Lambda and other AWS resource instrumentation. Must be \"true\" or \"false\"."
  default     = "true"
}

variable "collect_lambda_lifecycle_events" {
  type        = string
  description = "Whether to forward AWS Lambda lifecycle events (UpdateFunctionConfiguration, DeleteFunction) to Dash0 via EventBridge. Requires a CloudTrail trail logging management events in the selected regions. Must be \"true\" or \"false\"."
  default     = "true"
}

variable "technical_id" {
  type        = string
  description = "The Dash0 organization technical ID. Found in the Dash0 UI under Settings > Organization."
}

variable "regions" {
  type        = list(string)
  description = "AWS regions in which to enable AWS/Lambda metrics streaming (and Lambda lifecycle event forwarding when enabled). The list is joined into a comma-separated string for CloudFormation."
  default     = ["eu-west-1"]
}

variable "dash0_regional_endpoint" {
  type        = string
  description = "Regional Dash0 ingress endpoint base URL for your organization. Find it in the Dash0 UI under Settings > Endpoints > AWS CloudWatch Metrics (for example https://ingress.eu-west-1.aws.dash0.com). Provide only the base URL; the template appends the required paths internally."
}
