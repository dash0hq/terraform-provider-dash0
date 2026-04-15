data "aws_caller_identity" "current" {}

# Trust policy: allow the Dash0-owned AWS account to assume the role, but only
# when the caller presents the matching external ID.
data "aws_iam_policy_document" "trust" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "AWS"
      identifiers = ["arn:aws:iam::${var.dash0_aws_account_id}:root"]
    }
    condition {
      test     = "StringEquals"
      variable = "sts:ExternalId"
      values   = [var.external_id]
    }
  }
}

# -----------------------------------------------------------------------------
# Read-only role (always created): ViewOnlyAccess + Dash0ReadOnly inline policy.
# -----------------------------------------------------------------------------

resource "aws_iam_role" "readonly" {
  name               = "${var.iam_role_name_prefix}-read-only"
  assume_role_policy = data.aws_iam_policy_document.trust.json
  tags               = var.tags
}

resource "aws_iam_role_policy_attachment" "readonly_view_only_access" {
  role       = aws_iam_role.readonly.name
  policy_arn = "arn:aws:iam::aws:policy/job-function/ViewOnlyAccess"
}

resource "aws_iam_role_policy" "readonly_dash0" {
  name   = "Dash0ReadOnly"
  role   = aws_iam_role.readonly.name
  policy = file("${path.module}/policies/readonly.json")
}

# -----------------------------------------------------------------------------
# Instrumentation role (optional): a managed policy attached to a second role.
# The policy is prefix-scoped so multiple integrations can coexist in the same
# AWS account.
# -----------------------------------------------------------------------------

resource "aws_iam_role" "instrumentation" {
  count              = var.enable_instrumentation ? 1 : 0
  name               = "${var.iam_role_name_prefix}-instrumentation"
  assume_role_policy = data.aws_iam_policy_document.trust.json
  tags               = var.tags
}

resource "aws_iam_policy" "instrumentation" {
  count  = var.enable_instrumentation ? 1 : 0
  name   = "${var.iam_role_name_prefix}-lambda-instrumentation"
  policy = file("${path.module}/policies/instrumentation.json")
  tags   = var.tags
}

resource "aws_iam_role_policy_attachment" "instrumentation" {
  count      = var.enable_instrumentation ? 1 : 0
  role       = aws_iam_role.instrumentation[0].name
  policy_arn = aws_iam_policy.instrumentation[0].arn
}

# -----------------------------------------------------------------------------
# Register the integration with the Dash0 API.
# -----------------------------------------------------------------------------

resource "dash0_aws_integration" "this" {
  dataset                  = var.dataset
  external_id              = var.external_id
  aws_account_id           = data.aws_caller_identity.current.account_id
  read_only_role_arn       = aws_iam_role.readonly.arn
  instrumentation_role_arn = var.enable_instrumentation ? aws_iam_role.instrumentation[0].arn : null
}
