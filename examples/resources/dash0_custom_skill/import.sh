#!/bin/bash
# Agent0 custom skills are organization-scoped, so the import ID has no dataset prefix.
# A skill created by Terraform or the Dash0 CLI is imported by its origin;
# a skill created in the Dash0 UI has no origin and is imported by its id.
terraform import dash0_custom_skill.incident_triage tf_existing-custom-skill-origin
