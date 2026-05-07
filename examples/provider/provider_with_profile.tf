terraform {
  required_providers {
    dash0 = {
      source  = "dash0hq/dash0"
      version = "~> 1.6.0"
    }
  }
}

provider "dash0" {
  profile = "test1"
}
