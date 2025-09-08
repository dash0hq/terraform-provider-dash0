terraform {
  required_providers {
    dash0 = {
      source  = "dash0hq/dash0"
      version = "~> 1.4.0"
    }
  }
}

provider "dash0" {
  # Configuration can be provided via environment variables:
  # DASH0_URL and DASH0_AUTH_TOKEN
}
