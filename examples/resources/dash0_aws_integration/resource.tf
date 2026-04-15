# The dash0_aws_integration resource registers an AWS integration with the
# Dash0 API. It does NOT create IAM roles — you choose how roles are created
# and pass the ARNs in. Three paths are shown below; pick the one that fits.

############################################################
# Path 1 — Turnkey via the Dash0 AWS integration module
############################################################
#
# Recommended for most users. The module composes aws_iam_role + policies
# (via terraform-aws-modules/iam/aws) and registers with Dash0 for you.
# Pass `providers = { aws = aws }` to cascade `default_tags` from your root
# AWS provider, or pass an explicit `tags` variable.

# module "dash0_integration" {
#   source  = "dash0hq/dash0-aws-integration/aws"
#   version = "~> 1.0"
#
#   external_id            = var.dash0_org_id
#   dataset                = "default"
#   enable_instrumentation = true
#
#   # Optional: cascade provider default_tags
#   providers = { aws = aws }
# }

############################################################
# Path 2 — Direct resources (advanced / custom IAM)
############################################################
#
# You manage the aws_iam_role + policies yourself with the standard AWS
# provider, then register with Dash0. Use this when you need custom policies,
# lifecycle rules, or to attach additional policies to the roles.

data "aws_caller_identity" "current" {}

data "aws_iam_policy_document" "dash0_trust" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "AWS"
      identifiers = ["arn:aws:iam::115813213817:root"]
    }
    condition {
      test     = "StringEquals"
      variable = "sts:ExternalId"
      values   = [var.dash0_org_id]
    }
  }
}

resource "aws_iam_role" "dash0_readonly" {
  name               = "dash0-read-only"
  assume_role_policy = data.aws_iam_policy_document.dash0_trust.json
}

resource "aws_iam_role_policy_attachment" "dash0_readonly_view" {
  role       = aws_iam_role.dash0_readonly.name
  policy_arn = "arn:aws:iam::aws:policy/job-function/ViewOnlyAccess"
}

resource "dash0_aws_integration" "direct" {
  dataset            = "default"
  external_id        = var.dash0_org_id
  aws_account_id     = data.aws_caller_identity.current.account_id
  read_only_role_arn = aws_iam_role.dash0_readonly.arn

  # Optional: add the instrumentation role ARN when using Lambda auto-instrumentation
  # instrumentation_role_arn = aws_iam_role.dash0_instrumentation.arn
}

############################################################
# Path 3 — Pre-existing roles (platform-team-managed IAM)
############################################################
#
# Your platform team creates the IAM roles out-of-band and exposes the
# ARNs via variables, SSM, Vault, etc. Your Terraform only needs the Dash0
# provider — no AWS provider or AWS credentials required.

# variable "dash0_readonly_role_arn" { type = string }
# variable "dash0_instrumentation_role_arn" {
#   type    = string
#   default = null
# }
# variable "aws_account_id" { type = string }
#
# resource "dash0_aws_integration" "preexisting" {
#   dataset                  = "default"
#   external_id              = var.dash0_org_id
#   aws_account_id           = var.aws_account_id
#   read_only_role_arn       = var.dash0_readonly_role_arn
#   instrumentation_role_arn = var.dash0_instrumentation_role_arn
# }
