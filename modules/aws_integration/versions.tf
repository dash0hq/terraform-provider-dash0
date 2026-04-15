terraform {
  required_version = ">= 1.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0"
    }
    dash0 = {
      source  = "dash0hq/dash0"
      version = ">= 0.0.1"
    }
  }
}
