terraform {
  required_providers {
    dash0 = {
      source  = "dash0hq/dash0"
      version = "~> 1.6.0"
    }
  }
}

# The `dataset` attribute sets the default dataset for resources that omit
# their own `dataset` attribute. DASH0_DATASET and the dash0 CLI profile's
# dataset are consulted first; see the "Default dataset" section for the full
# precedence order.
provider "dash0" {
  dataset = "production"
}

resource "dash0_dashboard" "checkout" {
  # No dataset needed -- inherits "production" from the provider block.
  dashboard_yaml = file("checkout.yaml")
}

resource "dash0_dashboard" "staging_canary" {
  dataset        = "staging" # Still overridable per resource.
  dashboard_yaml = file("canary.yaml")
}
