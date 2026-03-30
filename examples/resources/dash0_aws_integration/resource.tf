# Basic AWS integration with read-only monitoring
resource "dash0_aws_integration" "monitoring" {
  dataset     = "default"
  external_id = "your-dash0-org-technical-id"
}

# AWS integration with Lambda auto-instrumentation enabled
resource "dash0_aws_integration" "full" {
  dataset     = "default"
  external_id = "your-dash0-org-technical-id"

  enable_resources_instrumentation = true
  iam_role_name_prefix             = "dash0"

  tags = {
    Environment = "production"
    ManagedBy   = "terraform"
  }
}
