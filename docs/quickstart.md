# Quickstart

This walkthrough takes you from a machine with only Terraform installed to a Dash0 check rule managed as code in about five minutes.
It covers declaring the provider, authenticating with a Dash0 auth token, applying your first `dash0_*` resource, and confirming the result in Dash0.

## Prerequisites

- Terraform >= 1.0 (or OpenTofu >= 1.6) on your `PATH`.
- A [Dash0](https://dash0.com/docs/dash0/get-started) organization with permission to create check rules.
- A Dash0 auth token — create one under [Settings → Auth Tokens](https://app.dash0.com/goto/settings/auth-tokens).
- The Dash0 API base URL for your region (for example, `https://api.us-west-2.aws.dash0.com`), from [Settings → Endpoints → API](https://app.dash0.com/goto/settings/endpoints?endpoint_type=api_http).

## 1. Set your credentials

Export the credentials as environment variables in the shell where you will run Terraform:

```sh
# Deep-link: https://app.dash0.com/goto/settings/endpoints?endpoint_type=api_http
export DASH0_API_URL="https://api.us-west-2.aws.dash0.com"
# Deep-link: https://app.dash0.com/goto/settings/auth-tokens — token starts with `auth_`.
export DASH0_AUTH_TOKEN="auth_xxxx"
```

The provider also accepts credentials from `provider`-block attributes or a Dash0 CLI profile — see [Configuration](configuration) for all supported sources.

## 2. Declare the provider

Create a new directory for your Terraform configuration and add a `main.tf` file with the provider requirement and an empty provider block:

```terraform
terraform {
  required_providers {
    dash0 = {
      source  = "dash0hq/dash0"
      version = "~> 1.6"
    }
  }
}

provider "dash0" {}
```

`provider "dash0" {}` intentionally has no attributes here — every setting is inherited from the environment variables you exported in step 1.

## 3. Declare your first resource

Append a `dash0_check_rule` resource to `main.tf`.
Replace `checkout-api` with the name of a service you already send telemetry for; the rule alerts when the service's span-level error rate exceeds `$__threshold` percent over any five-minute window.

```terraform
resource "dash0_check_rule" "checkout_error_rate" {
  dataset = "default"

  check_rule_yaml = <<-YAML
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: checkout-error-rate
spec:
  groups:
    - name: Alerting
      interval: 1m0s
      rules:
        - alert: checkout-error-rate
          expr: (sum by (service_name) (increase({otel_metric_name = "dash0.spans", service_name = "checkout-api", otel_span_status_code = "ERROR"}[5m]))) / (sum by (service_name) (increase({otel_metric_name = "dash0.spans", service_name = "checkout-api"}[5m])) > 0) * 100 > $__threshold
          for: 0s
          keep_firing_for: 0s
          annotations:
            summary: 'High error percentage for checkout-api: {{$value|printf "%.2f"}}%'
            dash0-threshold-critical: "5"
            dash0-threshold-degraded: "2"
            dash0-enabled: "true"
          labels: {}
YAML
}
```

Every `dash0_*` resource takes a YAML document as its primary attribute — the same format the Dash0 UI exports.
The `dataset` attribute selects which [Dash0 dataset](https://dash0.com/docs/dash0/miscellaneous/glossary/datasets) the asset belongs to; change it to match yours.

## 4. Initialize and apply

Download the provider and apply the configuration:

```sh
terraform init
```

```sh
terraform apply
```

Terraform prints a plan showing one resource to add, then prompts for confirmation.
Type `yes` and the provider creates the check rule via a single `PUT` to the Dash0 API.

## 5. Verify in Dash0

Open **Alerting → Check Rules** in the Dash0 UI.
The `checkout-error-rate` rule appears in the list with a `terraform` origin badge, and its details page shows the YAML you applied verbatim.
The rule starts evaluating on the interval defined in the manifest (one minute); once the service reports enough spans, the rule state transitions from *unknown* to *ok* or one of the threshold states.

Re-running `terraform apply` without changes reports *No changes*.
Editing the YAML — for example, tightening `dash0-threshold-critical` to `"3"` — and re-running `terraform apply` performs a create-or-replace `PUT` with the same origin, so the update is idempotent and preserves the resource identity.

## What's next

- **[Configuration](configuration)** — the full `provider` block schema, environment-variable reference, and Dash0 CLI profile options (including OAuth).
- **Resource reference** — the sibling pages under Resources document every `dash0_*` resource's schema, example usage, and import syntax.
- **[AWS integration via CloudFormation](guides/aws-cloudformation-integration)** — deploy the Dash0 AWS integration alongside your Terraform-managed Dash0 assets.
- **[About Managing as Code](https://dash0.com/docs/dash0/miscellaneous/manage-as-code/about-managing-as-code)** — when to reach for the Terraform Provider, the Dash0 Operator for Kubernetes, or the Dash0 CLI.
