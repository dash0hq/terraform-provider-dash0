# Manage which CloudWatch metrics Dash0 scrapes for an AWS integration.
# Each integration has exactly one scrape configuration, so declare at most one
# dash0_aws_metric_scrape_config per integration_id across all your Terraform
# configurations: two of them overwrite each other on every apply.
#
# Destroying this resource clears the scrape configuration (Dash0 stops
# scraping CloudWatch metrics for the integration); the integration stays.
resource "dash0_aws_metric_scrape_config" "prod" {
  integration_id                = "27755ed0-0009-422d-8c11-19c6dd72df98"
  aws_metric_scrape_config_yaml = <<-YAML
apiVersion: dash0.com/v1alpha1
kind: Dash0AwsMetricScrapeConfig
spec:
  namespaces:
    - namespace: AWS/Lambda
      metrics:
        - Errors
        - Invocations
    - namespace: AWS/SQS
      metrics:
        - ApproximateAgeOfOldestMessage
        - NumberOfMessagesSent
YAML
}

# An empty namespaces list keeps the resource but stops all scraping:
#
#   spec:
#     namespaces: []

# You can also load the YAML definition from a file:
#
# resource "dash0_aws_metric_scrape_config" "prod" {
#   integration_id                = "27755ed0-0009-422d-8c11-19c6dd72df98"
#   aws_metric_scrape_config_yaml = file("${path.module}/scrape-config.yaml")
# }
