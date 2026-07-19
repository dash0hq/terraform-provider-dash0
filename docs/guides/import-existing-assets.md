---
page_title: "Guide: Import existing Dash0 assets into Terraform"
subcategory: ""
description: |-
  Adopt dashboards, check rules, views, synthetic checks, recording rules, notification channels, and spam filters that already exist in Dash0 into Terraform state, without recreating them.
---

# Import existing Dash0 assets into Terraform

Most teams try Dash0 through the UI or the [Dash0 CLI](https://dash0.com/docs/dash0/miscellaneous/tooling/dash0-cli/about) before adopting the Terraform Provider.
This guide covers moving those existing assets into Terraform state, so that the provider becomes the source of truth without recreating anything server-side.

## What "import" does here

Every Dash0 asset has a stable identifier the API uses for GET/PUT/DELETE.
The Dash0 CLI surfaces it as the `ORIGIN` column of `dash0 <asset> list -o wide` when set, or falls back to the `ID` column otherwise — both are accepted by the API's read/write endpoints.
Under the JSON list output, the exact field varies by asset kind:

| Asset kind | JSON path that carries the identifier |
|-----------|---------------------------------------|
| Dashboards | `.metadata.dash0Extensions.id` |
| Views, synthetic checks, spam filters | `.metadata.labels["dash0.com/origin"]` (API-created) or `.metadata.labels["dash0.com/id"]` (UI-created; the origin label is absent) |
| Recording rules | `.metadata.labels["dash0.com/id"]` |
| Check rules | `.id` (top-level; check rules have no `metadata` field in the list response) |
| Notification channels | `.metadata.labels["dash0.com/id"]` |

For assets created by the Terraform Provider the identifier is a `tf_`-prefixed value; for assets created via the Dash0 UI or CLI it is a UUID (sometimes prefixed `api-`).

When Terraform imports an existing asset, it reads that identifier from Dash0 and keeps it verbatim in state; subsequent updates target the same identifier via `PUT`, so the asset is never recreated or renamed.

That preservation has one visible consequence: assets you imported keep whatever identifier Dash0 originally assigned them, while assets *newly created* by the Terraform Provider get `tf_`-prefixed origins.
After adopting a mix of both, the identifier prefix is no longer a reliable "managed by Terraform" indicator — the Terraform state file is.

## Prerequisites

- The Terraform Provider set up as in the [Quickstart](../quickstart) — credentials exported, `provider "dash0" {}` block declared.
  If you authenticate against Dash0 with an OAuth profile (`dash0 auth login`, token prefix `dash0_at_`) rather than a static `auth_` token, the Quickstart's env-var recipe will not work — use the [profile-based provider block](../configuration#option-3-dash0-cli-profile) instead so the provider can transparently refresh the access token.
- Terraform >= 1.5 (or OpenTofu >= 1.6) if you want to use declarative `import` blocks with config generation (recommended for more than a handful of assets).
- The [Dash0 CLI](https://dash0.com/docs/dash0/miscellaneous/tooling/dash0-cli/about) authenticated against the same organization and dataset — the walkthroughs below use it to discover origins and export YAML in one step.
  The Dash0 UI works too; the CLI is just scriptable.

## Step 1: Find the identifiers to import

The Dash0 CLI's wide-format list surfaces `NAME`, `ID`, `DATASET`, and `ORIGIN` for every asset in one table — good for eyeballing which assets to adopt:

```sh
dash0 dashboards list --dataset "$DATASET" -o wide --limit 500
```

The default `--limit` is 50, so bump it (or paginate) for any organization with more assets than that.
Use the value in the `ORIGIN` column when it is populated; when it is empty (typical for assets originally created in the Dash0 UI), use the `ID` column instead — the API accepts either.

The same pattern works for every dataset-scoped asset kind — `dash0 views list`, `dash0 check-rules list`, `dash0 synthetic-checks list`, `dash0 recording-rules list`, and `dash0 -X spam-filters list` (experimental commands need `-X` or `--experimental`).
Notification channels are organization-scoped, do not accept `--limit`, and do not offer `-o wide`; use `-o json` or `-o table` instead.

For scripting, the identifier field varies by asset kind (see the table in the previous section).
The examples below use dashboards and notification channels:

```sh
# Dashboards: identifier at .metadata.dash0Extensions.id
dash0 dashboards list --dataset "$DATASET" -o json --limit 500 \
  | jq -r '.[] | [.spec.display.name, .metadata.dash0Extensions.id] | @tsv'
```

```sh
# Notification channels: identifier at .metadata.labels["dash0.com/id"]
dash0 -X notification-channels list -o json \
  | jq -r '.[] | [.metadata.name, .metadata.labels["dash0.com/id"]] | @tsv'
```

For views, synthetic checks, and spam filters, replace the jq path with `.metadata.labels["dash0.com/origin"] // .metadata.labels["dash0.com/id"]` — the origin label is present on CLI/API/Terraform-created assets and absent on UI-created ones, and the `//` operator falls through to the id label when the origin one is null.
For check rules, the identifier lives at the top-level `.id` (check rules have no `metadata` wrapper in the list response).

See the [Asset CRUD commands reference](https://dash0.com/docs/dash0/miscellaneous/tooling/dash0-cli/commands#asset-crud-commands) for the full set of `list`/`get` flags and output formats.

Pick the identifiers you want to adopt into Terraform.
To script the flow end-to-end, capture one into a shell variable by filtering on the display name (adjust the jq path to match your target asset kind):

```sh
IDENTIFIER=$(dash0 dashboards list --dataset "$DATASET" -o json --limit 500 \
  | jq -r '.[] | select(.spec.display.name == "Checkout Overview") | .metadata.dash0Extensions.id')
```

The examples below reference `$IDENTIFIER` for that reason; substitute the literal value if you're running the commands by hand.

## Step 2 (single asset): imperative `terraform import`

For one or a handful of assets, the imperative flow is:

1. Export the current YAML from Dash0, so `terraform plan` after the import sees no diff:

   ```sh
   dash0 dashboards get "$IDENTIFIER" --dataset "$DATASET" -o yaml > checkout-overview.yaml
   ```

2. Declare a matching resource in Terraform, pointing at the exported file:

   ```terraform
   resource "dash0_dashboard" "checkout_overview" {
     dataset        = "default"
     dashboard_yaml = file("${path.module}/checkout-overview.yaml")
   }
   ```

   `${path.module}` resolves to the directory containing the `.tf` file, so run the `dash0 … get` command in step 2.1 from the same directory (or export to an absolute path and update the `file()` argument accordingly).

3. Import the resource into Terraform state.
   The import ID format is `dataset,identifier` — comma-separated, no whitespace:

   ```sh
   terraform import dash0_dashboard.checkout_overview "$DATASET,$IDENTIFIER"
   ```

4. Confirm parity.
   For an interactive check, run `terraform plan` and expect the "No changes" summary line.
   In a script or CI, use `-detailed-exitcode` — Terraform returns `0` when there is nothing to change, `2` when a diff is planned, and `1` on error:

   ```sh
   terraform plan -detailed-exitcode
   ```

   If the plan shows a diff, the exported YAML and the imported state disagree on some field the server rewrote — the provider normalizes both sides before comparison (stripping `metadata.labels`, `metadata.dash0Extensions`, and other server-managed metadata), so those never show up as drift.
   Any remaining diff is a real change worth resolving before you commit the file.

~> **Note:** For views specifically, assets originally created in the Dash0 UI carry `dash0.com/source: userdefined` and are read-only server-side. Import succeeds, `plan` reports no changes, but a subsequent `terraform apply` that modifies the YAML fails with `400 Bad Request: This operation does not support user-defined or built-in views.` Check with `dash0 views get "$IDENTIFIER" --dataset "$DATASET" -o yaml | grep dash0.com/source` before treating an imported view as fully manageable — if the source is `userdefined` or `builtin`, the view will need to be re-created via Terraform (destroy + apply) rather than updated in place, and only then will subsequent `terraform apply` calls work as expected.

Repeat for each asset.

## Step 2 (many assets): declarative `import` blocks

For dozens of assets, Terraform 1.5+'s declarative `import` block plus config generation removes the resource-shell boilerplate.

Write an `import.tf` file with one `import` block per asset:

```terraform
import {
  to = dash0_dashboard.checkout_overview
  id = "default,<identifier>"
}

import {
  to = dash0_dashboard.payments_overview
  id = "default,<other-identifier>"
}
```

Generate the corresponding resource blocks in one pass:

```sh
terraform plan -generate-config-out=generated.tf
```

Terraform writes matching `resource "dash0_dashboard" "…"` blocks into `generated.tf`, populated from what it read from Dash0.
The `dashboard_yaml` attribute lands as an inline `jsonencode({...})` block rather than the `file()` sidecar pattern shown in the imperative flow — refactor it to `file(...)` after the import if you prefer YAML sidecars.

The import blocks themselves are only *processed* by `terraform apply`, so:

1. Keep `import.tf` in place.
2. Run `terraform apply` — this performs the imports, populates state, and reports something like *Import complete! Resources: N imported.*
3. **Only after `terraform apply` succeeds**, delete `import.tf`.
   The imports have been recorded in state at that point.

~> **Note:** Deleting `import.tf` before `terraform apply` runs is the most common trap here. With no import blocks and no state, Terraform sees the resource blocks in `generated.tf` as brand-new resources and creates duplicate assets on the server with fresh `tf_`-prefixed origins. Delete `import.tf` only after `apply` reports the imports as complete.

You can script the `import` blocks themselves from the identifier listing in step 1.
Terraform identifiers must start with a letter, so the snippet below sanitizes `spec.display.name` (or falls back to `metadata.dash0Extensions.id`) and prefixes it with `d_`:

```sh
dash0 dashboards list --dataset "$DATASET" -o json --limit 500 \
  | jq -r --arg ds "$DATASET" '.[] | "import {\n  to = dash0_dashboard.d_\((.spec.display.name // .metadata.dash0Extensions.id) | ascii_downcase | gsub("[^a-z0-9]+"; "_") | sub("_+$"; ""))\n  id = \"\($ds),\(.metadata.dash0Extensions.id)\"\n}\n"' \
  > import.tf
```

The recipe above is dashboard-specific.
Adjust the jq path (`.metadata.dash0Extensions.id`) to match your target asset kind per the identifier table in [What "import" does here](#what-import-does-here).

Sanity-check the resource addresses in `import.tf` — the sanitizer may still collide if two assets share the same display name — and fix any duplicates by hand before running `terraform plan`.

## Notification channels: identifier only, no dataset

`dash0_notification_channel` is organization-scoped, not dataset-scoped, so the import ID drops the dataset prefix:

```sh
terraform import dash0_notification_channel.slack_alerts "<identifier>"
```

Every other asset kind — dashboards, check rules, views, synthetic checks, recording rules, and spam filters — uses the `dataset,identifier` shape.

## Verifying imported resources

After importing, `terraform plan` is the authoritative parity check.
Beyond that, three things are worth spot-checking:

- **Identifier preservation.** `terraform state show dash0_dashboard.checkout_overview` should print the identifier you imported (`origin = "..."`), not a fresh `tf_`-prefixed value.
- **`id` and `url` attributes.** Both are computed after import — `id` is Dash0's server-assigned UUID, `url` deep-links into the Dash0 web app.
  Neither round-trips into Dash0; they exist so other resources can reference the imported asset.
- **Cross-resource references.** A `dash0_check_rule` that names a `dash0_notification_channel` by its `id` in the `dash0.com/notification-channel-ids` annotation still resolves after an import — the channel's `id` attribute is populated during import, just as it is after a Terraform-driven create.

~> **Note:** A stubborn `plan` diff on a `dash0_spam_filter` import that survives re-exporting the YAML is almost always an `apiVersion` mismatch — the provider dispatches on it, so `apiVersion: v1alpha1` (the older `spec.contexts` list shape) and `apiVersion: v1alpha2` (the newer `spec.context` single-value shape) are treated as distinct schemas.

## What's next

- **[Configuration](../configuration)** — the full `provider` block schema and authentication reference.
- **Resource reference** — the sibling pages under Resources document per-resource schemas and `terraform import` syntax.
- **[About Managing as Code](https://dash0.com/docs/dash0/miscellaneous/manage-as-code/about-managing-as-code)** — the higher-level page on managing Dash0 assets across the Terraform Provider, the Dash0 Operator for Kubernetes, and the Dash0 CLI.
