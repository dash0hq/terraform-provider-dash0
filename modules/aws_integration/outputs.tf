output "aws_account_id" {
  description = "The AWS account ID where the IAM roles were created."
  value       = data.aws_caller_identity.current.account_id
}

output "read_only_role_arn" {
  description = "ARN of the Dash0 read-only IAM role."
  value       = aws_iam_role.readonly.arn
}

output "instrumentation_role_arn" {
  description = "ARN of the Dash0 instrumentation IAM role, or null when enable_instrumentation = false."
  value       = var.enable_instrumentation ? aws_iam_role.instrumentation[0].arn : null
}

output "integration_id" {
  description = "Composite ID of the dash0_aws_integration resource ({aws_account_id}-{external_id})."
  value       = dash0_aws_integration.this.id
}
