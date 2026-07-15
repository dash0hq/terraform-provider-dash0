terraform {
  required_providers {
    dash0 = {
      source  = "dash0hq/dash0"
      version = "~> 1.6.0"
    }
  }
}

provider "dash0" {
  # Configuration will be read from environment variables:
  # DASH0_API_URL (or DASH0_URL as fallback) and DASH0_AUTH_TOKEN
}
