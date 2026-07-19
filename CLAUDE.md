# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build commands

- Build: `make build`
- Run all tests: `make test`
- Run specific test: `go test -v github.com/dash0hq/terraform-provider-dash0/internal/provider -run TestName`
- Run tests with Terraform API: `make testacc`
- Generate docs: `make docs`
- Clean build artifacts: `make clean`
- Install provider locally: `make install`

## Roundtrip tests (test/roundtrip/)

Dockerized end-to-end tests that verify each Terraform resource type against the real Dash0 API.
Each test creates a resource via `tofu apply`, downloads it via the `dash0` CLI and checks YAML equivalence, updates it, verifies idempotency, destroys it, and confirms deletion.

- Run all: `make test-roundtrip`
- Run one: `./test/roundtrip/run_all.sh test_dashboard.sh`
- Locally: Docker running, dash0 CLI configured with an active profile (`~/.dash0/`)
- CI: set `DASH0_API_URL`, `DASH0_AUTH_TOKEN`, and optionally `DASH0_DATASET` env vars (no CLI profile needed)

### Structure

- `Dockerfile` — multi-stage build: compiles the provider, bundles OpenTofu, dash0 CLI, Python + PyYAML
- `common.sh` — shared helpers (credentials from env vars, tofu wrappers, `assert_yaml_equivalent`, `tf_import`)
- `test_<resource>.sh` — one script per resource type (check_rule, dashboard, recording_rule_group, synthetic_check, view)
- `test_import_<resource>.sh` — `terraform import` roundtrip tests that create the asset out-of-band via the dash0 CLI, adopt it into Terraform state, and assert `terraform plan` reports no changes. Every asset kind has an import test (check_rule, dashboard, notification_channel, recording_rule, spam_filter, synthetic_check, view); they all follow the 8-step template in `test_import_dashboard.sh` and diverge only in the CLI subcommand and identifier JSON path per kind.
- `run_all.sh` — host-side script that builds the image, resolves credentials (env vars or `~/.dash0/`), and runs each test in a fresh container

### Adding a new roundtrip test

When a new resource type is added to the provider:

1. Create `test/roundtrip/test_<resource>.sh` following the pattern of existing tests (create, verify via CLI, update, idempotency, destroy, verify deletion).
2. Include a YAML equivalence check at step 2 using `assert_yaml_equivalent`.
3. Add the new script name to the `TESTS` array in `run_all.sh`.
4. If the resource has server-managed fields beyond the standard set (`id`, `dash0.com/origin`, `dash0.com/dataset`, `dash0.com/version`), update `strip_server_fields` in `common.sh`.

## Architecture

This is a Terraform provider for managing Dash0 monitoring resources as code: dashboards, check rules, synthetic checks, views, and recording rule groups.

- Written in Go using the [Terraform Plugin Framework](https://developer.hashicorp.com/terraform/plugin/framework).
- Uses the [Dash0 API Client Go](https://github.com/dash0hq/dash0-api-client-go) as its foundation for all Dash0 API interactions.
- Published to the Terraform and OpenTofu registries.

### File organization

| Directory / file | Purpose |
|------------------|---------|
| `internal/provider/` | Provider configuration and resource/data-source registration |
| `internal/provider/client/` | Per-domain API client wrappers (one file per asset type) |
| `internal/converter/yaml_json.go` | YAML-to-JSON conversion via `ConvertYAMLToJSON()` |
| `internal/converter/normalizer.go` | YAML normalization for drift detection |
| `examples/` | Reference Terraform configurations for each resource/data source |
| `docs/` | Generated documentation published to registries |

### Origin handling

The provider is ID-agnostic at the YAML level.
It generates its own origin with a `tf_` prefix and a random UUID, stored as a computed attribute in Terraform state:

```go
origin = "tf_" + uuid.New().String()
```

All CRUD operations use `PUT /api/{asset-type}/{origin}?dataset={dataset}` with this origin in the URL path.
The provider never uses POST for creation — it always uses PUT with create-or-replace semantics.

### YAML-to-JSON conversion

YAML documents are converted to JSON via `ConvertYAMLToJSON()` in `internal/converter/yaml_json.go` and sent as-is in the request body.
The provider does not parse or extract ID fields from the YAML content.
If an ID is present in the YAML, it is sent to the backend but ignored (the origin in the URL path takes precedence).

### State normalization and drift detection

For drift detection, the provider normalizes YAML by stripping server-generated fields (`internal/converter/normalizer.go`).
The stripped fields include `metadata.labels`, `metadata.dash0Extensions`, and `metadata.annotations`, which means any ID fields added by the backend are excluded from state comparison and do not cause spurious diffs in Terraform plans.

## Adding a new resource

This is the most common evolution.
Follow the patterns established by dashboards, check rules, views, synthetic checks, and recording rule groups.

### Prerequisites

A Terraform resource or data source can only expose a Dash0 platform capability if the underlying API is already consumable through the [Dash0 API Client Go](https://github.com/dash0hq/dash0-api-client-go).
If the API is documented in the [Dash0 API reference](https://api-docs.dash0.com/reference) but not yet in the client library, add it there first.

### Implementation steps

1. Add a new API client wrapper in `internal/provider/client/<domain>.go`, following the pattern of existing files:
   - `internal/provider/client/dashboard.go`
   - `internal/provider/client/view.go`
   - `internal/provider/client/check_rule.go`
   - `internal/provider/client/synthetic_check.go`
   - `internal/provider/client/recording_rule_group.go`
2. Define the resource (and optionally a data source) in `internal/provider/`, using Terraform Plugin Framework types (`types.String`, and so on) for all schema attributes.
3. Implement the full CRUD lifecycle:
   - **Create**: Generate a `tf_`-prefixed origin, convert YAML to JSON, call `PUT` with the origin.
   - **Read**: Call `GET` by origin, normalize the response YAML for state storage.
   - **Update**: Convert YAML to JSON, call `PUT` with the existing origin.
   - **Delete**: Call `DELETE` by origin.
4. Implement Terraform import support.
5. Add example configurations in `examples/` for the new resource and data source.
6. Add roundtrip tests. Two scripts are required:
   - `test/roundtrip/test_<resource>.sh` — create-via-Terraform lifecycle (see "Adding a new roundtrip test" above).
   - `test/roundtrip/test_import_<resource>.sh` — `terraform import` adoption of an out-of-band asset. Model after `test_import_dashboard.sh` (dataset-scoped, `dataset,identifier` import ID) or `test_import_notification_channel.sh` (org-scoped, `identifier` alone). The identifier's JSON path in the CLI's `list -o json` response varies by asset kind — see the identifier table in `docs/guides/import-existing-assets.md` before writing the discovery jq.
7. Run `make docs` to regenerate documentation. Verify that a new file `docs/resources/<resource>.md` was created and contains the schema, example usage, and import instructions. If the file is missing, check that the resource has example configurations in `examples/resources/dash0_<resource>/` (at minimum `resource.tf` and `import.sh`) and a doc template in `templates/resources/` if needed.
8. Update the "Managed assets" list in `docs/about.md` (the curated dash0.com/docs landing page) so it includes the new resource — one bullet with the resource name, link to `resources/<slug>`, and a short description. Then add the file to `.github/workflows/sync-docs/transformations.yaml` under `files:` with a `source`, `target` (hyphenated slug under `resources/`), `title`, and `description`. See "Documentation sync to dash0.com/docs" below.
9. Run `make test` to verify the build and all tests pass.

## Documentation sync to dash0.com/docs

The `docs/` tree feeds two audiences:

- **Terraform and OpenTofu registries** consume `docs/index.md`, `docs/resources/*.md`, `docs/guides/*.md`. `docs/index.md` and the resource pages are regenerated by `make docs` (terraform-plugin-docs) and must not be edited by hand — edit the resource `Description` strings and the `templates/index.md.tmpl` file instead.
- **[dash0.com/docs](https://www.dash0.com/docs)** consumes a curated subset defined in `.github/workflows/sync-docs/transformations.yaml`. The dash0.com/docs landing page for the provider is `docs/about.md`; the first-run narrative walkthrough is `docs/quickstart.md`; the full provider-configuration reference is `docs/configuration.md`. All three are **hand-authored files** that are *not* touched by `make docs`. `docs/index.md` is intentionally *not* synced to dash0.com/docs because it duplicates the per-resource pages and would send readers off-site for content that has an in-site equivalent.

Both flows share the same generated `docs/resources/*.md` and `docs/guides/*.md` files, so schema and guide content stays in one place.

### Keeping `docs/about.md` current

`docs/about.md` is a short orientation page — who the provider is for, which assets it manages, its design principles, and how it relates to the Dash0 Operator and CLI. It intentionally does **not** repeat the per-resource schema or example usage; those live in `docs/resources/*.md`.

Update `docs/about.md` when:

- A new resource is added → add a bullet to "Managed assets" with the resource name, the `resources/<hyphenated-slug>` link, and a one-line description.
- A resource is removed or renamed → remove or rename the corresponding "Managed assets" bullet.
- Design principles change materially (new drift-detection behavior, non-`PUT` semantics for some operations, and so on) → update the "Design principles" section.
- The relationship to sibling tooling changes (Dash0 Operator, Dash0 CLI, Agent0 IaC generation) → update the "Related tooling" section.

### Keeping `docs/quickstart.md` current

`docs/quickstart.md` is a task-driven walkthrough — five minutes from "Terraform installed" to "first `dash0_*` resource applied and visible in Dash0". Its scope is deliberately narrow: one resource kind, one auth source (env vars), one apply. Do **not** grow it into a tour of every feature — that is what the Resources reference is for.

Update `docs/quickstart.md` when:

- The default first-resource example stops working (metric name changed, PromQL syntax changed, resource schema changed) → adapt the walkthrough and re-verify by running `terraform init && terraform apply` end-to-end against dash0-dev.
- The recommended default auth source changes (environment variables are the recommended default today) → update step 1 and add a callout referencing `configuration.md` for the other sources.
- The `terraform apply` output shape or origin badge in the Dash0 UI changes → update the verification step so the reader knows what success looks like.

Prose rules mirror those of `docs/about.md`: semantic line breaks, one command per code block (a multi-line command continued with `\` or a pipeline counts as one), and internal links use dash0.com/docs URL conventions (hyphens, no `.md` suffix).

### Keeping `docs/configuration.md` current

`docs/configuration.md` documents the `provider` block schema, the supported environment variables, credential resolution order, and OAuth-enabled Dash0 CLI profiles. It is the authoritative in-site reference for provider configuration on dash0.com/docs.

**It duplicates the authentication content in `templates/index.md.tmpl` (rendered into `docs/index.md`).** Whenever either file changes, update the other in the same PR:

- Adding, removing, or renaming a `provider` block attribute → update the "Provider block schema" table in `configuration.md` **and** the corresponding section in `templates/index.md.tmpl`.
- Adding, removing, or renaming a `DASH0_*` environment variable → update the "Environment variables" table in `configuration.md` **and** the equivalent table in `templates/index.md.tmpl`.
- Changing OAuth or CLI-profile behavior → update the "OAuth-enabled profiles" section in both files.

If the two ever drift, `docs/configuration.md` is the source of truth for dash0.com/docs readers; the registry-rendered `docs/index.md` mirrors it for Terraform Registry readers.

Prose rules for `docs/about.md`:

- One sentence per line (semantic line breaks).
- Relative links use dash0.com/docs URL conventions — hyphenated slugs and no `.md` suffix (for example, `resources/check-rule`, not `resources/check_rule.md`). These links do not resolve when the file is viewed on GitHub, and that is intentional — the file is authored for the website. The GitHub-facing entry point is `README.md`.
- External links to `https://dash0.com/docs/...` **must** be verified via the [Dash0 Knowledge Base MCP server](#referencing-dash0-documentation) before committing, and `make lint-links` **must** pass.

### Editing the sync configuration

`.github/workflows/sync-docs/transformations.yaml` is the single source of truth for what gets published:

- **`files:`** is opt-in. Adding a new source file to `docs/` does nothing until it is declared here.
- **`common:`** transformations run against every file, in order. Today they strip the terraform-plugin-docs frontmatter (marked `required: false` because hand-authored pages have none), strip the leading `# ...` heading (sync-docs-action replaces it with the page's frontmatter title), rewrite absolute `https://dash0.com/docs/...` URLs to site-internal `/docs/...` paths, and rewrite `~> **Note:**` admonitions to GitHub-style `> [!NOTE]` blocks.
- Per-file `transformations:` handle source-vs-target divergences that only affect one page. None are needed today — the common rules cover every current divergence.

### Admonition convention

Source pages use **Terraform-registry-native admonitions** (`~> **Note:** ...`, `-> **Tip:** ...`, `!> **Warning:** ...`). Every established provider on the Terraform and OpenTofu registries uses this syntax; GitHub-style `> [!NOTE]` blocks render as plain blockquotes with the literal `[!NOTE]` text visible on the Registry, which looks broken. The `common:` rewrite in `transformations.yaml` converts `~>` admonitions to `> [!NOTE]` on the way into dash0.com/docs.

**Keep admonitions to a single line.** The rewrite operates one line at a time — multi-line `~>` blocks (Registry allows continuation lines under the same admonition) survive the transformation but their continuations do not get the `> ` prefix that GitHub-style admonitions require, so they'd render as a `[!NOTE]` block plus loose prose on dash0.com/docs. If a note needs more than one sentence's worth of content, put the essential warning in the `~>` line and follow it with a normal paragraph.
- **`nav:`** emits `nav.json` alongside the synced pages. Bump `order` only in coordination with `dash0hq/dash0-website` — the value determines where the "Dash0 Terraform Provider" section appears under "Miscellaneous → Tooling".

The sync engine runs in dry-run mode on every CI build (`.github/workflows/ci.yml` → `sync-docs-to-website-dry-run`) — if a `replace-regex` rule stops matching (source docs drifted away from what the transformation expects), CI fails. Investigate the drift and either fix the source doc or update the transformation; do not silence the rule with `required: false` unless the drift is intentional.

The full sync (dry-run false) runs from `.github/workflows/release.yml` after `goreleaser` publishes a release tag. It opens a PR against `dash0hq/dash0-website` using the `DOCS_WEBSITE_PR_TOKEN` secret — a fine-grained PAT scoped to `dash0hq/dash0-website` with `contents:write` and `pull-requests:write`. This is distinct from the broader `REPOSITORY_FULL_ACCESS_GITHUB_TOKEN` that GoReleaser and `prepare-release.yml` use to push artifacts and tags back to this repo.

## Changelog

This project uses [chloggen](https://github.com/open-telemetry/opentelemetry-go-build-tools/tree/main/chloggen) to manage changelog entries.
Every user-facing PR must include a `.chloggen/*.yaml` entry. PRs prefixed with `chore:`, labeled `Skip Changelog`, or from dependabot are exempt.

- Create a new entry: `make chlog-new` (creates a YAML file named after the current branch)
- Validate entries: `make chlog-validate`
- Preview the changelog: `make chlog-preview`

### Writing a changelog entry

Fill in the generated `.chloggen/<branch>.yaml` file:

- `change_type`: one of `breaking`, `deprecation`, `new_component`, `enhancement`, `bug_fix`
- `component`: the area of concern (e.g. `dashboards`, `check_rules`, `views`, `provider`, `synthetic_checks`, `recording_rule_groups`)
- `note`: a brief, user-facing description of the change
- `issues`: list of related issue or PR numbers (e.g. `[42]`)
- `subtext`: (optional) additional detail, rendered indented below the note

Do not edit `CHANGELOG.md` directly — it is generated automatically during the release process.

## Validating changes

Before considering a change complete, run `make all` which executes all validation steps in order:

1. `make build` — verify the project compiles.
2. `make lint` — run Go, shell, and link linters (`lint-go`, `lint-sh`, `lint-links`). Fix all issues before proceeding.
3. `make test-unit` — run all unit tests.
4. `make test-roundtrip` — run the Dockerized roundtrip tests against the real Dash0 API. These catch integration issues (serialization mismatches, server-side validation failures, idempotency regressions) that unit tests cannot.

All four must pass. Do not skip any step.

`make lint-links` runs [lychee](https://github.com/lycheeverse/lychee) against `docs/`, `templates/`, `internal/`, `examples/`, `README.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `CHANGELOG.md`, and `.chloggen/`. Configuration (skip patterns, accepted status codes, scanned extensions, cache settings) lives in `lychee.toml` at the repo root. Network access is required; 401/403/429 are accepted as success (auth-protected URLs). The lychee binary is installed on demand into `.tools/lychee` by `make lint-links-install` (uses an existing `lychee` on PATH if present, otherwise downloads the pinned `LYCHEE_VERSION` from GitHub releases). Successful checks are cached at `.lycheecache` (gitignored) for 1 day so consecutive runs only re-hit failed or stale URLs.

## Referencing Dash0 documentation

The Terraform provider's user-facing strings (resource `Description` fields, doc templates, README, CHANGELOG entries) routinely link into the Dash0 docs at `https://dash0.com/docs/...`. The docs site reorganizes occasionally, and **link paths must never be guessed or written from memory** — `make lint-links` exists specifically to catch this and will fail the build.

Whenever you add or update a `https://dash0.com/docs/...`, `https://dash0.com/changelog/...`, `https://dash0.com/blog/...`, or `https://www.dash0.com/...` URL:

1. Verify the canonical URL via the Dash0 Knowledge Base MCP server — call `mcp__dash0-prod__searchKnowledgeBase` (or one of the equivalent `dash0-dev` / `Dash0_Knowledge_Base` tools) with terms describing the destination page. The tool returns documents with their authoritative URLs.
2. Use the URL returned by the MCP search verbatim. Do not infer category prefixes (e.g. `data-management` vs `cost-control`) or page slugs from sibling URLs in the codebase — both have changed in the past.
3. If no MCP server is available, run `make lint-links` before committing so broken URLs surface early. Never rely on a URL just because it "looks plausible" or appears in another resource's description.
4. After editing a resource `Description`, run `make docs` so the generated `docs/resources/*.md` mirrors the new URL.

## Code style

- Follow standard Go formatting with `gofmt`.
- Use Go 1.26+ features and conventions.
- Group imports: standard library first, then third-party packages.
- Always handle errors with appropriate context using `fmt.Errorf("message: %w", err)`.
- Use Terraform Plugin Framework types (`types.String`, and so on) for all resource attributes.
- Prefix logging with context: `tflog.Debug(ctx, "message")`.
- Use interfaces for testability (see `dash0ClientInterface`).
- Use meaningful comments for exported functions and types.
- Prefer explicit error checking over helper functions that hide errors.
- Test both success and error cases in unit and integration tests.

## Guidelines

- Use the [Dash0 API Client Go](https://github.com/dash0hq/dash0-api-client-go) for all Dash0 API interactions — never implement raw HTTP calls.
- Do not use `.Inner()` on the API client unless there is a very compelling reason.
- Reuse `Client` instances keyed on the tuple `(authToken, apiUrl, otlpEndpoint)`.
- Follow the [Terraform Provider development best practices](https://developer.hashicorp.com/terraform/plugin/best-practices) from HashiCorp.
- Resource schemas must be backward compatible across minor versions.
- All resources must support full CRUD lifecycle and import.
- Documentation is published to the Terraform and OpenTofu registries — keep it in sync with resource schemas.
- Add example configurations in `examples/` for every new resource or data source.
