# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

<!-- next version -->

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
