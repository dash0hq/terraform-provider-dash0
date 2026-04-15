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

# Register the AWS integration with Dash0
resource "dash0_aws_integration" "monitoring" {
  dataset            = "default"
  external_id        = var.dash0_org_id
  aws_account_id     = data.aws_caller_identity.current.account_id
  read_only_role_arn = aws_iam_role.dash0_readonly.arn
}
