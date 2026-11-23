# Forward the CloudWatch log groups of an AWS integration that carry matching tags.
# Each integration has at most one policy, so declare at most one
# dash0_aws_log_group_policy per integration_id.
#
# A log group is forwarded when its own tags match any rule; every condition of
# a rule must hold. Tag keys are written as dash0.cloud.tags.<tag name>.
resource "dash0_aws_log_group_policy" "prod" {
  integration_id = "27755ed0-0009-422d-8c11-19c6dd72df98"

  # What destroying this resource does to the log groups it forwards:
  # "keep" (the default) keeps them forwarding, "remove" removes their
  # subscription filters. Apply the change before you destroy.
  deletion_disposition = "keep"

  aws_log_group_policy_yaml = <<-YAML
apiVersion: dash0.com/v1alpha1
kind: Dash0AwsLogGroupInstrumentationPolicy
spec:
  onNoLongerMatching: keep
  rules:
    - conditions:
        - key: dash0.cloud.tags.team
          operator: is_one_of
          values: [checkout, payments]
        - key: dash0.cloud.tags.env
          operator: is
          value: production
    - conditions:
        - key: dash0.cloud.tags.dash0-forward
          operator: is_set
YAML
}
