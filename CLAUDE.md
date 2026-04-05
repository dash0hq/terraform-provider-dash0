# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Test Commands

- Build: `make build`
- Run all tests: `make test`
- Run specific test: `go test -v github.com/dash0hq/terraform-provider-dash0/internal/provider -run TestName`
- Run tests with Terraform API: `make testacc`
- Generate docs: `make docs`
- Clean build artifacts: `make clean`
- Install provider locally: `make install`

## Roundtrip Tests (test/roundtrip/)

Dockerized end-to-end tests that verify each Terraform resource type against the real Dash0 API.
Each test creates a resource via `tofu apply`, downloads it via the `dash0` CLI and checks YAML
equivalence, updates it, verifies idempotency, destroys it, and confirms deletion.

- Run all: `./test/roundtrip/run_all.sh`
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
1. Create `test/roundtrip/test_<resource>.sh` following the pattern of existing tests (create → verify via CLI → update → idempotency → destroy → verify deletion)
2. Include a YAML equivalence check at step 2 using `assert_yaml_equivalent`
3. Add the new script name to the `TESTS` array in `run_all.sh`
4. If the resource has server-managed fields beyond the standard set (`id`, `dash0.com/origin`, `dash0.com/dataset`, `dash0.com/version`), update `strip_server_fields` in `common.sh`

## Code Style Guidelines

- Follow standard Go formatting with `gofmt`
- Use Go 1.26+ features and conventions
- Group imports: standard library first, then third-party packages
- Always handle errors with appropriate context using `fmt.Errorf("message: %w", err)`
- Use terraform-plugin-framework types (types.String, etc.) for all resource attributes
- Prefix logging with context: `tflog.Debug(ctx, "message")`
- Use interfaces for testability (see `dash0ClientInterface`)
- Use meaningful comments for exported functions and types
- Prefer explicit error checking over helper functions that hide errors
- Test both success and error cases in unit/integration tests