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

## Code Style Guidelines

- Follow standard Go formatting with `gofmt`
- Use Go 1.25+ features and conventions
- Group imports: standard library first, then third-party packages
- Always handle errors with appropriate context using `fmt.Errorf("message: %w", err)`
- Use terraform-plugin-framework types (types.String, etc.) for all resource attributes
- Prefix logging with context: `tflog.Debug(ctx, "message")`
- Use interfaces for testability (see `dash0ClientInterface`)
- Use meaningful comments for exported functions and types
- Prefer explicit error checking over helper functions that hide errors
- Test both success and error cases in unit/integration tests