# Look up a team that Terraform does not manage, for example a team synced
# from your identity provider, by its display name. The match is exact and
# case-sensitive. The lookup fails when no team has the name, or when more
# than one team has it. In that case, look the team up by id instead.
data "dash0_team" "payments" {
  name = "Payments"
}

# Grant the team access to a dataset.
resource "dash0_dataset" "payments" {
  dataset_yaml = <<-YAML
apiVersion: dash0.com/v1alpha1
kind: Dash0Dataset
metadata:
  name: Payments
spec:
  permissions:
    - teamId: ${data.dash0_team.payments.id}
      actions: [dataset:read]
YAML
}

# Look up a team by id instead of by name.
data "dash0_team" "platform" {
  id = "team_01h455vb4pex5vsknk084sn02q"
}
