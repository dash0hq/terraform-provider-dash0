terraform {
  required_providers {
    dash0 = {
      source  = "dash0hq/dash0"
      version = "~> 1.6.0"
    }
  }
}

# The `profile` attribute loads credentials from a named dash0 CLI profile
# (configured via `dash0 config profiles create`). Omit it to fall back to the
# CLI's active profile.
provider "dash0" {
  profile = "test1"
}
