# A custom skill with a SKILL.md and a reference file.
# The skill's name comes from the `name` field of the SKILL.md frontmatter.
resource "dash0_custom_skill" "incident_response" {
  custom_skill_yaml = <<-YAML
kind: Dash0Skill
spec:
  files:
    - relativePath: SKILL.md
      content: |
        ---
        name: incident-response
        description: Triage an incident with the SRE team's runbook.
        ---
        When asked to triage an incident, follow references/runbook.md step by step.
    - relativePath: references/runbook.md
      content: |
        1. Check the error rate of the affected service.
        2. Check the deployments of the last hour.
        3. Post a summary in the incident channel.
YAML
}

# A custom skill shared with a team, which may also delete it.
# spec.permissions replaces the stored grants exactly; omit it to keep the server defaults.
# Grants on a Terraform-managed skill accept custom_skill:read and custom_skill:delete,
# never custom_skill:write.
resource "dash0_custom_skill" "release_notes" {
  custom_skill_yaml = <<-YAML
kind: Dash0Skill
spec:
  files:
    - relativePath: SKILL.md
      content: |
        ---
        name: release-notes
        description: Write release notes in the house style.
        ---
        Write release notes as a bulleted list, one bullet per user-facing change.
  permissions:
    - role: basic_member
      actions: [custom_skill:read]
    - teamId: "<YOUR_TEAM_ID>"
      actions: [custom_skill:read, custom_skill:delete]
YAML
}

# You can also build the YAML from a directory of skill files kept next to the configuration:
#
# resource "dash0_custom_skill" "from_directory" {
#   custom_skill_yaml = yamlencode({
#     kind = "Dash0Skill"
#     spec = {
#       files = [
#         for f in fileset("${path.module}/skills/incident-response", "**") : {
#           relativePath = f
#           content      = file("${path.module}/skills/incident-response/${f}")
#         }
#       ]
#     }
#   })
# }
