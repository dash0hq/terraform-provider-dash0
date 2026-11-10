---
page_title: "Guide: AWS Integration via CloudFormation"
subcategory: ""
description: |-
  Deploy the Dash0 AWS integration using the official CloudFormation template, managed via Terraform.
---

# AWS Integration via CloudFormation

This guide shows how to deploy the Dash0 AWS integration using Terraform's [`aws_cloudformation_stack`](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudformation_stack) resource.

~> **Note:** This is not a native Dash0 provider resource. It uses the AWS provider to deploy a CloudFormation stack with the official Dash0 integration template.

The integration provisions the required IAM roles and configuration for collecting telemetry from your AWS account.
When `EnableLambdaIntegration` is enabled (default), the template additionally provisions, via a CloudFormation StackSet, per-region resources for the `AWS/Lambda` namespace — a Kinesis Firehose delivery stream, a CloudWatch metric stream and an S3 backup bucket — and an EventBridge rule, ApiDestination, and Connection that forward Lambda lifecycle events (`UpdateFunctionConfiguration`, `DeleteFunction`) to Dash0 in near real-time.

## Prerequisites

- An active [Dash0](https://dash0.com) organization.
- A Dash0 **API key** (auth token), created under **Settings > Auth Tokens** in the Dash0 UI.
- Your Dash0 **regional endpoint base URL**, found under **Settings > Endpoints > AWS CloudWatch Metrics** in the Dash0 UI (for example `https://ingress.eu-west-1.aws.dash0.com`).
  Provide only the base URL — the template appends the required paths internally.
- An AWS account with permissions to create CloudFormation stacks, StackSets, and IAM roles.
- Terraform >= 1.0 and the [AWS provider](https://registry.terraform.io/providers/hashicorp/aws/latest) >= 5.0.
- To collect Lambda lifecycle events (enabled by default), a CloudTrail trail logging management events in each region listed in `Regions`.
  Most AWS accounts already have an org-wide trail; if yours does not, either enable one or set `EnableLambdaIntegration` to `"false"`.

## Example Usage

A complete, runnable example is available in [`examples/guides/aws_cloudformation_integration/`](https://github.com/dash0hq/terraform-provider-dash0/tree/main/examples/guides/aws_cloudformation_integration).

The core resource definition is:

```terraform
resource "aws_cloudformation_stack" "dash0_integration" {
  name         = "dash0-integration"
  template_url = "https://public-integrations-production.eu-west-1.aws.dash0.com.s3.eu-west-1.amazonaws.com/dash0-customer-integration-cloudformation-v3.yaml"

  parameters = {
    ApiKey                  = var.api_key
    Dataset                 = var.dataset
    EnableMetricsScraping   = var.enable_metrics_scraping
    EnableLambdaIntegration = var.enable_lambda_integration
    TechnicalId             = var.technical_id
    Regions                 = join(",", var.regions)
    Dash0RegionalEndpoint   = var.dash0_regional_endpoint
  }

  capabilities = ["CAPABILITY_NAMED_IAM", "CAPABILITY_AUTO_EXPAND"]
}
```

The `CAPABILITY_NAMED_IAM` capability is required because the template creates named IAM roles for the Dash0 integration.
The `CAPABILITY_AUTO_EXPAND` capability is required because the v3 template includes a nested `AWS::CloudFormation::StackSet` that deploys the per-region Dash0 Lambda integration resources (metrics streaming and lifecycle event forwarding) when `EnableLambdaIntegration` is enabled.

CloudFormation expects `Regions` to be a `CommaDelimitedList`, so the Terraform list variable is joined into a comma-separated string before being passed to the stack.

## CloudFormation outputs

The integration template exposes the following outputs through `aws_cloudformation_stack.dash0_integration.outputs`:

| Output | Description | Availability |
|--------|-------------|--------------|
| `IntegrationId` | UUID of the AWS integration registered in Dash0. Use this as `dash0_aws_metric_scrape_config.integration_id`. | Always |
| `ReadOnlyRoleARN` | ARN of the read-only IAM role used by Dash0. | Always |
| `ResourcesInstrumentationRoleARN` | ARN of the Lambda instrumentation IAM role. | When `EnableLambdaIntegration` is `"true"` |
| `RegionalStackSetId` | ID of the StackSet that deploys the per-region Lambda integration resources. | When `EnableLambdaIntegration` is `"true"` |
| `TemplateVersion` | Version of the deployed onboarding template. | Always |

`IntegrationId` is the Dash0 integration UUID, not the CloudFormation stack ID, AWS account ID, or `TechnicalId` IAM external ID.
The output key is case-sensitive.
The example exposes it as a Terraform output:

```terraform
output "integration_id" {
  description = "The Dash0 AWS integration ID."
  value       = aws_cloudformation_stack.dash0_integration.outputs["IntegrationId"]
}
```

After applying, read it with `terraform output -raw integration_id`.
For an existing stack, check the **Outputs** tab in the CloudFormation console for `IntegrationId`.
If it is missing, update the stack to a template version that exposes this output.

## Configure CloudWatch metric scraping with Terraform

Set `EnableMetricsScraping` to `"true"` on the stack, then use [`dash0_aws_metric_scrape_config`](https://dash0.com/docs/dash0/miscellaneous/tooling/terraform-provider-dash0/resources/aws-metric-scrape-config) to choose the namespaces and metrics to collect.
Add the Dash0 provider to your existing `required_providers` block alongside the AWS provider:

```terraform
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0"
    }
    dash0 = {
      source = "dash0hq/dash0"
    }
  }
}

provider "dash0" {}
```

Configure the Dash0 provider with `DASH0_API_URL` and `DASH0_AUTH_TOKEN`, as described in the [provider configuration documentation](https://registry.terraform.io/providers/dash0hq/dash0/latest/docs).
`DASH0_API_URL` is the Dash0 API endpoint, not the regional ingress base URL passed to the CloudFormation template.
Use credentials for the same Dash0 organization as the stack's `ApiKey`.

Reference the stack output directly instead of copying a UUID:

```terraform
resource "dash0_aws_metric_scrape_config" "prod" {
  integration_id               = aws_cloudformation_stack.dash0_integration.outputs["IntegrationId"]
  aws_metric_scrape_config_yaml = <<-YAML
    apiVersion: dash0.com/v1alpha1
    kind: Dash0AwsMetricScrapeConfig
    spec:
      namespaces:
        - namespace: AWS/SQS
          metrics:
            - ApproximateAgeOfOldestMessage
            - NumberOfMessagesSent
  YAML
}
```

This reference makes Terraform wait for the stack to finish registering the integration before applying its scrape configuration.
No separate `depends_on` or manual UUID lookup is needed when both resources are in the same configuration.
The scrape configuration belongs to the integration's dataset, selected through the stack's `Dataset` parameter.
Declare only one scrape configuration per integration, because each apply replaces the complete configuration.
Destroying the scrape configuration stops scraping but does not delete the integration.

If the stack and scrape configuration are managed in separate Terraform configurations, expose `integration_id` from the stack configuration and pass that value to the configuration managing `dash0_aws_metric_scrape_config`.
Apply the stack configuration first, because Terraform does not infer dependencies across separate states.

## CloudFormation Parameters

| Parameter | Description | Required | Default |
|-----------|-------------|----------|---------|
| `ApiKey` | Dash0 API key (auth token) for the integration. | Yes | -- |
| `Dataset` | The Dash0 dataset to send telemetry data to. | No | `default` |
| `EnableMetricsScraping` | Let Dash0 read CloudWatch metrics and resource tags in the selected regions (`"true"` or `"false"`). | No | `"true"` |
| `EnableLambdaIntegration` | Enable the Lambda instrumentation role, stream AWS/Lambda metrics, and forward Lambda lifecycle events (`UpdateFunctionConfiguration`, `DeleteFunction`) to Dash0 via EventBridge (`"true"` or `"false"`). Lifecycle events require a CloudTrail trail logging management events in the selected regions. | No | `"true"` |
| `TechnicalId` | External ID for securing IAM role assumption. When empty, the template uses the CloudFormation stack ID automatically — the recommended approach for IaC onboarding. | No | `""` |
| `Regions` | Comma-separated list of AWS regions in which to enable `AWS/Lambda` metrics streaming and (when enabled) Lambda lifecycle event forwarding (for example `eu-west-1,us-east-1`). | Yes | -- |
| `Dash0RegionalEndpoint` | Regional Dash0 ingress endpoint base URL for your organization. Find it under **Settings > Endpoints > AWS CloudWatch Metrics** in the Dash0 UI (for example `https://ingress.eu-west-1.aws.dash0.com`). Provide only the base URL; the template appends the required paths internally. | Yes | -- |

## Getting Started

1. Copy the [example directory](https://github.com/dash0hq/terraform-provider-dash0/tree/main/examples/guides/aws_cloudformation_integration) into your project, or use the resource definition above in your own configuration.

2. Create a `terraform.tfvars` file with your values (do **not** commit this file):

   ```hcl
   api_key                 = "your-dash0-auth-token"
   dash0_regional_endpoint = "https://ingress.eu-west-1.aws.dash0.com"
   regions                 = ["eu-west-1"]
   ```

3. Initialize and apply:

   ```shell
   terraform init
   terraform plan
   terraform apply
   ```

4. To tear down the integration:

   ```shell
   terraform destroy
   ```

## Avoid repeated API-key changes

CloudFormation hides the integration's API key when Terraform reads the stack, returning `****` instead of the token.
As a result, Terraform can report an API-key change even when you have not changed the token.
This is a [known AWS provider issue](https://github.com/hashicorp/terraform-provider-aws/issues/29405).

To suppress this change during routine applies, add the following block inside `aws_cloudformation_stack.dash0_integration`:

```terraform
lifecycle {
  ignore_changes = [parameters["ApiKey"]]
}
```

This rule suppresses the API-key diff, not changes to the rest of the stack.
It also prevents Terraform from applying a new API key, including one updated in Secrets Manager.

~> **Note:** Before applying any change to this stack, temporarily remove the API-key ignore rule and supply a valid token. Otherwise, Terraform can send the masked value `****` as the API key and the update can fail. This applies to region changes, tags, template updates, and token rotation.

### Update the stack: add a region

Use this sequence whenever you need to update a stack that has the ignore rule above.
For example, to add `eu-central-1` while keeping `eu-west-1`:

1. **Temporarily remove the API-key ignore rule.**
   Remove `parameters["ApiKey"]` from `ignore_changes`.
   If it is the only entry, remove the entire `lifecycle` block shown above.
   Do not remove `ApiKey` from the `parameters` map.

2. **Make sure Terraform has a valid API key.**
   Check the value supplied through `api_key` or your secret source, such as Secrets Manager.
   Use the actual token, not the masked value `****`.

3. **Add the region to your configuration.**
   Keep the existing regions and append the new one:

   ```hcl
   regions = ["eu-west-1", "eu-central-1"]
   ```

4. **Review a fresh plan.**
   Do not reuse a plan saved before removing the ignore rule.

   ```shell
   terraform plan
   ```

   Expect an in-place update to `Regions` and an API-key change, which Terraform normally hides as a sensitive value.
   The API-key change is expected even if you are reusing the same valid token.

5. **Apply and verify the update.**

   ```shell
   terraform apply
   ```

   Wait for the parent CloudFormation stack to reach `UPDATE_COMPLETE`.
   If the Lambda integration is enabled, also check that the StackSet instance in the added region is `CURRENT` with a detailed status of `SUCCEEDED`.

6. **Restore the API-key ignore rule.**
   Add the `lifecycle` block above back to the stack resource, then check the plan again:

   ```shell
   terraform plan
   ```

   With no other configuration changes, Terraform should now report no changes to this stack.
   Keep this rule in your configuration for routine applies, and remove it again before the next stack update.

### Rotate the API key

Follow the same sequence, but replace the token in step 2 instead of changing the regions in step 3.
Keep the old token valid until the stack update completes and you have verified that the integration works with the replacement.
Then revoke the old token and restore the ignore rule.
Updating the token in Secrets Manager alone does not update the integration while the rule is enabled.

## Notes

- The `api_key` variable is marked as `sensitive` so Terraform will not display its value in plan or apply output.
- The `TechnicalId` parameter is optional.
  When omitted or left empty, the template derives the IAM external ID from the CloudFormation stack ID.
  This is the recommended approach for Terraform onboarding — no Dash0-internal identifier is required.
- The CloudFormation stack is region-specific.
  Deploy it in the region where your AWS workloads run, or in the region closest to your Dash0 environment.
  The `Regions` parameter controls which regions the nested StackSet provisions Lambda metrics streaming into; it is independent from the region the parent stack itself is deployed in.
- The S3 backup bucket created by the StackSet is retained on `terraform destroy` by design.
  Delete it manually if you want to fully reclaim the bucket name before re-onboarding.
