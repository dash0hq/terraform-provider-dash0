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
- `common.sh` — shared helpers (credentials from env vars, tofu wrappers, `assert_yaml_equivalent`)
- `test_<resource>.sh` — one script per resource type (check_rule, dashboard, recording_rule_group, synthetic_check, view)
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
6. Add a roundtrip test in `test/roundtrip/test_<resource>.sh` (see "Adding a new roundtrip test" above).
7. Run `make docs` to regenerate documentation.
8. Run `make test` to verify the build and all tests pass.

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

Before considering a change complete, run:

1. `make build` — verify the project compiles.
2. `make test-unit` — run all unit tests.
3. `make test-roundtrip` — run the Dockerized roundtrip tests against the real Dash0 API. These catch integration issues (serialization mismatches, server-side validation failures, idempotency regressions) that unit tests cannot.
4. `make lint` — run Go and shell linters. Fix all issues before proceeding.

All four must pass. Do not skip any step.

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
