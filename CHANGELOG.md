# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

<!-- next version -->

## 1.16.1


### Bug Fixes


- `spam_filters`: Treat a 404 from `Read` as the spam filter no longer existing, removing it from state instead of failing `plan`/`apply`. (#164)

- `spam_filters`: Fix `dash0_spam_filter` create/update/delete calls to the same dataset failing with an unretried 409 dataset version conflict when a single `terraform apply` changes more than one spam filter at once. (#165)
  Terraform's default parallelism issues concurrent Create/Update/Delete calls for spam filters in the same
  dataset, racing the dataset's optimistic-concurrency version. The provider now serializes spam filter writes
  per dataset so the race never reaches the API, instead of surfacing the conflict as an unretried error.
  The serialization covers every provider instance in a run, so aliased provider blocks writing the same
  dataset are serialized against each other too. Writers outside the Terraform process — a concurrent
  apply, the Dash0 web app, the CLI, or the Operator — are still able to race the dataset version.
  

## 1.16.0


### New Components


- `actions`: Add the `dash0_deployment_event` and `dash0_log_event` actions for emitting deployment markers and arbitrary log events from Terraform (#157)
  `dash0_deployment_event` emits a `dash0.deployment` event so a deploy driven by `terraform apply`
  shows up as a dashboard annotation; `dash0_log_event` is its general-purpose counterpart, mirroring
  the `dash0 logs send` CLI command. Attach either to a resource with a `lifecycle` `action_trigger`
  block, or invoke it standalone with `terraform apply -invoke`. Actions require Terraform 1.14 or
  later; the provider is unaffected on older versions.
  
  Both need the new optional `otlp_url` provider attribute (or the `DASH0_OTLP_URL` environment
  variable, or a dash0 CLI profile's `otlpUrl`), which addresses the Dash0 OTLP ingress endpoint
  rather than the API. Resources are unaffected and continue to work without it.
  
  Delivery failures (the event was well-formed but Dash0 could not be reached) are reported as
  warnings rather than errors by default, so a transient ingestion problem cannot fail an apply or
  block a resource an action is attached to. Set `fail_on_error = true` to opt into treating them as
  errors. Configuration mistakes (missing `otlp_url`, an OAuth-enabled profile — the Dash0 OTLP
  ingress endpoint does not accept OAuth access tokens even though the API does — an empty `body`, or
  malformed `trace_id`/`span_id`) always fail regardless of `fail_on_error`, since none of them
  survive a retry.
  


### Enhancements


- `notification_channels`: Document that `spec.routing.assets` is a read-only, server-derived back-reference in the routing example and the import guide (#128)
  The Dash0 API populates `spec.routing.assets` when a check rule or synthetic check binds to the
  channel and discards any value supplied on write; the provider already warns when it is set and
  excludes it from drift comparison. The routing example and the import guide now say so explicitly.
  


### Bug Fixes


- `check_rules`: Merge a `check_rule_yaml` document's top-level `metadata.annotations` into the rule's own annotations, matching the Dash0 Kubernetes operator and CLI (#153)
  Previously a setting defined once at the top level of a PrometheusRule document was dropped
  rather than applied, so `dash0.com/notification-channel-ids` set there never reached the check
  rule and notification routing did not happen. A rule's own annotations still take precedence
  when the same key is set in both places. The example in
  `examples/resources/dash0_check_rule/resource.tf` has used a top-level annotation since before
  this fix, so configurations copied from it were affected.
  

- `provider`: Refresh OAuth access tokens throughout a plan or apply, and fix named OAuth profiles never refreshing (#161)
  Credentials from an OAuth-enabled Dash0 CLI profile were captured once, when the provider
  was configured, then reused for the rest of the run. A Dash0 OAuth access token lasts 15
  minutes, so a plan or apply over a large configuration failed with 401 partway through. The
  token is now refreshed as needed for the whole run, which is what the documentation already
  described.
  
  A named profile, `provider "dash0" { profile = "production" }`, had it worse: it was never
  refreshed, so it failed as soon as the stored token was over 15 minutes old. Only the active
  profile was refreshed, and only at configure time. Named profiles now behave the same.
  
  Static credentials from `DASH0_AUTH_TOKEN` or the `auth_token` attribute are unaffected.
  

## 1.15.0


### New Components


- `teams`: Add team resource for managing teams and membership as code (#133)
  New `dash0_team` resource declaratively manages Dash0 teams — the technical name, display
  attributes, and membership — via the `TeamDefinitionV1Alpha1` CRD envelope. Members can be
  referenced by email address or internal Dash0 id; the server resolves emails during
  reconciliation, and the provider rewrites returned ids back to emails on read so state
  matches the user's YAML. Teams are organization-level resources and are not scoped to a
  dataset.
  


### Bug Fixes


- `provider`: Populate `id` and `url` in state after `terraform import` for assets that carry no `dash0.com/origin` label. (#135)
  Assets originally created in the Dash0 UI have no `dash0.com/origin` label, so the previous
  identifier resolver — which matched only on origin — left the imported resource's `id` and `url`
  attributes empty (only a `TF_LOG=INFO` warning surfaced the miss). The resolver now falls back to
  matching the imported identifier against each list item's internal id, so `id` and `url` are
  populated for both CLI/API/Terraform-created assets (where the origin label is present) and
  UI-created assets (where it is absent). Applies to dashboards, views, check rules, synthetic
  checks, recording rules, spam filters, and notification channels.
  

## 1.14.2


### Enhancements


- `provider`: Add AWS CloudFormation integration guide and Terraform example for IaC onboarding. (#76)
  Documents deploying the Dash0 AWS integration via the AWS provider's
  `aws_cloudformation_stack` resource and the hosted v2 CloudFormation template.
  `TechnicalId` is optional for Terraform onboarding — when omitted, the template
  derives the IAM external ID from the CloudFormation stack ID (template 2.2 / CLO-906).
  

## 1.14.1


### Bug Fixes


- `notification_channels`: Resolve perpetual drift on dash0_notification_channel when a check rule or synthetic check binds to the channel by id. (#128)
  The Dash0 API populates `spec.routing.assets` on a notification channel as a
  back-reference whenever a check rule (via the
  `dash0.com/notification-channel-ids` annotation) or a synthetic check (via
  `spec.notifications.channels`) binds to the channel by id. The field is
  discarded on write, so previous releases produced a perpetual diff that
  attempted to wipe the back-reference on every plan. The provider now treats
  `spec.routing.assets` as API-managed and ignores it during comparison.
  
  If a user supplies `spec.routing.assets` on the channel YAML, the provider
  emits a warning on create and update, since the Dash0 API will discard the
  value. Bind a check rule or synthetic check to a channel from the check
  resource instead.
  

## 1.14.0


### Enhancements


- `provider`: Support OAuth-enabled dash0 CLI profiles (#123)
  The provider now accepts profiles authenticated via `dash0 auth login` (OAuth).
  Access tokens are transparently refreshed when close to expiry.
  If the refresh token is expired, a clear error directs the user to re-authenticate.
  

- `provider`: Load Dash0 credentials from a dash0 CLI profile when they are not supplied via attributes or environment variables. (#65)
  Adds an optional `profile` attribute on the provider block and reads from
  the dash0 CLI configuration directory (`~/.dash0` by default, overridable
  via `DASH0_CONFIG_DIR`). Credentials are resolved in this order:
  
    1. `DASH0_API_URL` / `DASH0_AUTH_TOKEN` environment variables (`DASH0_URL`
       remains accepted as a deprecated fallback for the URL).
    2. The `url` / `auth_token` provider attributes.
    3. The CLI profile named by the `profile` attribute; if `profile` is not
       set, the active profile in the dash0 CLI configuration directory.
  

## 1.13.0


### Enhancements


- `provider`: Expose `id` as a computed attribute on every resource (#119)
  All provider resources — dashboards, views, check rules, synthetic checks,
  recording rules, spam filters, and notification channels — now expose a
  computed `id` attribute holding the server-assigned UUID. Reference it (e.g.
  as `${dash0_notification_channel.example.id}`) when wiring one resource's
  identifier into another resource's YAML, where the Dash0 API expects raw
  UUIDs rather than provider-generated origins.
  

## 1.12.0


### Enhancements


- `resources`: Add a computed `url` attribute to the `dash0_dashboard`, `dash0_check_rule`, `dash0_synthetic_check`, `dash0_view` and `dash0_notification_channel` resources that links to the resource in the Dash0 web app (#115)
  The URL is derived from the configured Dash0 API URL and the resource's server-assigned
  identifier. For views, the page is selected based on the view's type. It may be empty for
  self-hosted deployments whose web app uses a custom domain that cannot be derived from the
  API URL.
  

## 1.11.0


### Enhancements


- `provider`: Add `DASH0_MAX_RETRIES` environment variable (#141)
  Configures the maximum number of retries for failed API requests.
  Accepted values: 0–5. Default: 3. Behavior before this change: 1 retry.
  

## 1.10.3


### Enhancements


- `provider`: Document presence on the OpenTofu registry and add Terraform/OpenTofu registry badges to the README. (#101)

- `spam_filters`: Support the `v1alpha2` spam filter shape (`spec.context` scalar) in addition to the existing `v1alpha1` shape (`spec.contexts` list). (#105)
  The provider now dispatches Create/Update against the matching API endpoint based on the
  `apiVersion` declared in `spam_filter_yaml`, and decodes Read responses into either shape.
  

## 1.10.2


### Enhancements


- `dashboards, check_rules, synthetic_checks, views`: `dash0.com/sharing` and `dash0.com/folder-path` metadata annotations are now preserved during drift detection (#98)
  The `dash0.com/sharing` annotation is supported on dashboards, check rules, synthetic checks, and views.
  The `dash0.com/folder-path` annotation is supported on dashboards and views.
  Changes to these annotations now trigger a Terraform plan update. All other metadata annotations remain server-managed and are ignored during drift detection.
  

## 1.10.1


### Bug Fixes


- `spam_filters`: Fix spam filter FilterCriteria format in fixtures, tests, and docs to match the Dash0 AttributeFilter API schema (#96)
  The filter criteria now uses the correct flat format with `operator` and `value` fields
  instead of the incorrect nested `stringValue` format.
  

## 1.10.0


### New Components


- `spam_filters`: Add new `dash0_spam_filter` resource for managing spam filters as code (#93)

## 1.9.1


### Bug Fixes


- `recording_rule_groups`: Fix dataset query parameter not being sent for recording rule create/update API calls (#87)
  Updated dash0-api-client-go to include the fix for passing the dataset query parameter on POST and PUT recording rule endpoints.

## 1.9.0


### New Components


- `recording_rules`: Add `dash0_recording_rule` resource for managing recording rules using the PrometheusRule CRD format (#82)


### Bug Fixes


- `provider`: Detect `metadata.name` changes as drift (#84)
  Previously `YAMLSemanticEqual` treated `metadata.name` as ignorable along with
  server-managed fields (labels, annotations, timestamps). Because resources are
  identified by the `origin` UUID (not the name), a rename in the user's config
  was silently suppressed at plan time — the resource stayed in state under its
  old name while the config asked for a new one. Rename operations via config
  now surface as a plan diff and apply correctly, for every resource type
  (`dash0_check_rule`, `dash0_dashboard`, `dash0_notification_channel`,
  `dash0_synthetic_check`, `dash0_view`).
  

## 1.8.0


### New Components


- `notification_channels`: Add notification channel resource for managing alert delivery integrations (#77)
  New `dash0_notification_channel` resource supporting all channel types (webhook, Slack, email,
  PagerDuty, Opsgenie, Microsoft Teams, Discord, Google Chat, and more). Includes routing rules
  for filtering notifications by labels. Notification channels are organization-level resources
  and are not scoped to a dataset.
  

## 1.7.2


### Enhancements


- `provider`: Rebase provider on dash0-api-client-go and add roundtrip tests (#73)
  Replace the internal check rule converter and model packages with the dash0-api-client-go/yaml
  library, removing duplicated YAML-to-API conversion logic. Add Dockerized roundtrip tests that
  verify each resource type end-to-end against the real Dash0 API.
  


### Bug Fixes


- `views`: Fix flaky view roundtrip test caused by unstable YAML comparison and eventual consistency (#75)
  Replace fmt.Sprint-based sort keys in ResourceYAMLEquivalent with a canonical string function
  that recursively sorts nested structures, ensuring stable order-independent YAML comparison.
  Add retry-based helpers (assert_idempotent, assert_yaml_equivalent_eventually) to tolerate
  eventual consistency of server-managed fields like permissions. Extract the duplicated
  idempotency check pattern into the shared assert_idempotent helper.
  

## v1.7.1 (2026-02-19)

- fix(SUP-678): conditionally ignore spec.permissions and null values in YAML normalization (#53)

## v1.7.0 (2026-02-16)

- build(deps): bump actions/checkout from 6.0.1 to 6.0.2 (#48)
- build(go): skip deprecated GOOS=windows GOARCH=arm build
- build(go): update to 1.26
- feat: add release version to User-Agent request header (#49)
- fix: make YAML semantic comparison resilient to round-trip formatting differences (#51)

## v1.6.3 (2026-01-19)

- build(deps): bump actions/setup-go from 6.1.0 to 6.2.0
- fix: treat zero-value threshold annotations as semantically equivalent

## v1.6.2 (2026-01-19)

- build(deps): bump actions/checkout from 6.0.0 to 6.0.1
- build(deps): bump golang.org/x/sync from 0.18.0 to 0.19.0
- build(deps): bump golangci/golangci-lint-action from 9.1.0 to 9.2.0
- build(deps): bump the terraform group with 2 updates
- build(deps): update Go to 1.25.6 and bump dependencies
- fix: resolve YAML idempotency issue for check_rule and synthetic_check resources
- fix: thresholds of check rules are floating point numbers (#45)
- fix: use RequiresReplace for dataset changes and simplify YAML normalization

## v1.6.1 (2025-11-24)

- build(deps): bump actions/checkout from 5.0.0 to 6.0.0
- build(deps): bump actions/setup-go from 6.0.0 to 6.1.0
- build(deps): bump github.com/hashicorp/terraform-plugin-log
- build(deps): bump golang.org/x/sync from 0.17.0 to 0.18.0
- build(deps): bump golangci/golangci-lint-action from 8.0.0 to 9.0.0
- build(deps): bump golangci/golangci-lint-action from 9.0.0 to 9.1.0
- chore(deps): update to go 1.25.4

## v1.6.0 (2025-10-28)

- feat: add url and auth_token as optional provider configuration attributes
- feat: make sure that environment variables take precedence over configuration

## v1.5.3 (2025-10-20)

- SUP-214: incomplete terraform documentation missing what customers (#30)
- build(deps): bump github.com/hashicorp/terraform-plugin-docs
- fix: update synthetic check locations

## v1.5.2 (2025-10-14)

- build(deps): bump github.com/hashicorp/terraform-plugin-framework
- build(deps): bump the terraform group with 3 updates
- build: update to go 1.25
- fix: handle `dash0-enabled` annotation correctly
- refactor: adjust naming to be closer to golang best practices
- refactor: move client code into subpackage
- refactor: move resource models into subpackage
- refactor: remove redundant query code from client package
- refactor: seperate converter/normalizer code into a subpackage

## v1.5.1 (2025-09-08)

- fix: change repo link

## v1.5.0 (2025-09-08)

## v1.4.0 (2025-09-08)

- build(deps): bump actions/checkout from 4.2.2 to 5.0.0
- build(deps): bump actions/setup-go from 5.4.0 to 5.5.0
- build(deps): bump actions/setup-go from 5.5.0 to 6.0.0
- build(deps): bump github.com/stretchr/testify from 1.8.3 to 1.11.1
- build(deps): bump golang.org/x/sync from 0.12.0 to 0.16.0
- build(deps): bump golang.org/x/sync from 0.16.0 to 0.17.0
- build(deps): bump goreleaser/goreleaser-action from 6.3.0 to 6.4.0
- build(deps): bump the terraform group across 1 directory with 4 updates
- feat: add support for check rules
- feat: support views (#18)
- fix: make normalizer ignore different ordering in slices/arrays (#19)

## v1.3.0 (2025-08-22)

- feat: support synthetic checks (#12)

## v1.2.1 (2025-05-02)

- fix: update generated docs

## v1.2.0 (2025-05-02)

## v1.1.0 (2025-05-02)

## v1.0.0 (2025-05-01)

- perf: Limit HTTP client to 10 concurrent requests

## v0.1.1 (2025-05-01)

- build(deps): bump crazy-max/ghaction-import-gpg from 6.2.0 to 6.3.0
- improve: Add small delay before calling API to avoid rate limiting (#4)

## v0.1.0 (2025-04-30)

- build(deps): bump actions/setup-go from 5.2.0 to 5.4.0 (#2)
- build(deps): bump goreleaser/goreleaser-action from 6.2.1 to 6.3.0 (#1)
- feat: support dashboard state import

## v0.0.5 (2025-04-30)

## v0.0.4 (2025-04-30)

## v0.0.3 (2025-04-30)

## v0.0.2 (2025-04-30)

- fix: CRUD for dashboards

## v0.0.1 (2025-04-30)

- feat: initial setup
