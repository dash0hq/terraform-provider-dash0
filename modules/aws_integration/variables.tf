variable "external_id" {
  description = "The Dash0 organization technical ID (also called the organization ID in Dash0's UI). Used as the STS AssumeRole external ID in the IAM trust policy."
  type        = string
  validation {
    condition     = length(var.external_id) > 0
    error_message = "external_id must be a non-empty string."
  }
}

variable "dataset" {
  description = "The Dash0 dataset slug to associate with this integration."
  type        = string
  default     = "default"
}

variable "iam_role_name_prefix" {
  description = "Prefix for IAM role names. The read-only role is created as '<prefix>-read-only'; the instrumentation role (if enabled) as '<prefix>-instrumentation'."
  type        = string
  default     = "dash0"
}

variable "enable_instrumentation" {
  description = "Whether to create the instrumentation role for Lambda auto-instrumentation in addition to the read-only role."
  type        = bool
  default     = false
}

variable "dash0_aws_account_id" {
  description = "The Dash0-owned AWS account ID that assumes the IAM roles. Defaults to Dash0's production account; override only if directed by Dash0 support."
  type        = string
  default     = "115813213817"
}

variable "tags" {
  description = "Tags applied to the IAM roles and the instrumentation policy. Leave empty and rely on `default_tags` from the `aws` provider (passed via `providers = { aws = aws }`) if you prefer global tagging. Both can be combined — AWS merges them."
  type        = map(string)
  default     = {}
}
