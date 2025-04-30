#!/usr/bin/env bash

set -eo pipefail

rm -rf .terraform .terraform.lock.hcl terraform.tfstate
terraform init
TF_LOG=debug terraform plan
TF_LOG=debug terraform apply
