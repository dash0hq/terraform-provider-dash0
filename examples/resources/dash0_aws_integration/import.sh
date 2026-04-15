# Import an existing AWS integration.
# Format: "dataset,aws_account_id,external_id"
# Note: none of the three values may contain a comma — the import ID is parsed with strings.Split(",").
terraform import dash0_aws_integration.example "default,123456789012,your-dash0-org-technical-id"
