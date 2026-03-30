# Import an existing AWS integration.
# Format: "dataset,external_id[,iam_role_name_prefix]"
# The iam_role_name_prefix defaults to "dash0" if not specified.
terraform import dash0_aws_integration.example "default,your-dash0-org-technical-id"

# Import with custom prefix
terraform import dash0_aws_integration.example "default,your-dash0-org-technical-id,my-prefix"
