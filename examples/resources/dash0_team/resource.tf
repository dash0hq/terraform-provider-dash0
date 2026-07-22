# Manage a Dash0 team declaratively. Teams group organization members so
# alerts, dashboards, and other assets can be attributed to a shared owner.
# Teams are organization-level resources (no dataset).
#
# Members can be referenced by email address (matched case-insensitively
# against organization members) or by internal Dash0 id (the `dash0.com/id`
# value returned by the Members API). Email references are the recommended
# style because they are legible in diffs and reviews.
resource "dash0_team" "backend" {
  team_yaml = <<-YAML
kind: Dash0Team
metadata:
  name: backend-team
spec:
  display:
    name: Backend Team
    description: Owns backend services and the data platform.
    color:
      from: "#6366F1"
      to: "#8B5CF6"
  members:
    - alice@example.com
    - bob@example.com
YAML
}

# Expose the server-assigned team id when other resources need to reference
# the team by its raw UUID.
output "backend_team_id" {
  value = dash0_team.backend.id
}

# You can also load the YAML definition from a file:
#
# resource "dash0_team" "backend" {
#   team_yaml = file("${path.module}/teams/backend-team.yaml")
# }
