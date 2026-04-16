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
  description = "The Dash0 organization technical ID. Found in the Dash0 UI under Settings > Organization."
}
