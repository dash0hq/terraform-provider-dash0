# About the Dash0 Terraform Provider

The Dash0 Terraform Provider manages Dash0 observability assets — dashboards, alerting rules, saved views, synthetic checks, notification channels, spam filters, and teams — as Terraform resources.
It is published on the [Terraform](https://registry.terraform.io/providers/dash0hq/dash0/latest) and [OpenTofu](https://search.opentofu.org/provider/dash0hq/dash0/latest) registries.

## Managed assets

Each Dash0 asset kind is exposed as a Terraform resource whose primary attribute is a YAML document using the same formats exported from the Dash0 UI or the [Dash0 CLI](https://dash0.com/docs/dash0/miscellaneous/tooling/dash0-cli/about).

- [`dash0_dashboard`](resources/dashboard) — Perses dashboards.
- [`dash0_check_rule`](resources/check-rule) — Prometheus-style alerting rules.
- [`dash0_recording_rule`](resources/recording-rule) — Prometheus recording rule groups.
- [`dash0_view`](resources/view) — saved telemetry queries.
- [`dash0_synthetic_check`](resources/synthetic-check) — HTTP-based availability probes.
- [`dash0_notification_channel`](resources/notification-channel) — Slack, email, PagerDuty, Opsgenie, webhook, Microsoft Teams, Discord, and Google Chat destinations.
- [`dash0_spam_filter`](resources/spam-filter) — ingestion-time telemetry filters.
- [`dash0_team`](resources/team) — organization-level teams that group members and own assets.

## Authentication

The provider accepts credentials from three sources, checked in order:

1. Environment variables (`DASH0_API_URL`, `DASH0_AUTH_TOKEN`).
2. Provider-block attributes (`url`, `auth_token`).
3. A named [Dash0 CLI](https://dash0.com/docs/dash0/miscellaneous/tooling/dash0-cli/about) profile — including OAuth-enabled profiles created with `dash0 auth login`, which the provider refreshes automatically before every request.

See [Configuration](configuration) for the full `provider` block schema, the supported environment variables, and end-to-end examples for each authentication source.

## Related tooling

- The [Dash0 Operator for Kubernetes](https://dash0.com/docs/dash0/monitoring/kubernetes/dash0-operator/overview) manages the same assets as `PersesDashboard`, `PrometheusRule`, and Dash0-specific custom resources — the right choice for GitOps flows already anchored on `kubectl apply` and ArgoCD or Flux.
- The [Dash0 CLI](https://dash0.com/docs/dash0/miscellaneous/tooling/dash0-cli/about) scripts the same operations for one-off migrations, batch deployments, or shell-driven automation.
- See [About Managing as Code](https://dash0.com/docs/dash0/miscellaneous/manage-as-code/about-managing-as-code) for a side-by-side of when to pick which.

## Next steps

- **[Quickstart](quickstart)** — a five-minute walkthrough that declares the provider, authenticates, and applies your first `dash0_check_rule`.
- **[Configuration](configuration)** — the full `provider` block schema and environment-variable reference.
- **Resource reference** — the sibling pages under Resources document the schema, example usage, and import syntax of each resource type.
- **[AWS integration via CloudFormation](guides/aws-cloudformation-integration)** — deploy the Dash0 AWS integration alongside your Terraform-managed Dash0 assets.
- **Provider source** — [`github.com/dash0hq/terraform-provider-dash0`](https://github.com/dash0hq/terraform-provider-dash0) for issues, changelog, and contributions.
