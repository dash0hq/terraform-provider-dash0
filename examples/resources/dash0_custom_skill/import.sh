#!/bin/bash
# Agent0 custom skills are organization-scoped, so the import ID has no dataset prefix.
# Import a skill by its origin when it has one, otherwise by its id. Skills
# created in the Dash0 UI, or with the Dash0 CLI using a user token, have no origin.
terraform import dash0_custom_skill.incident_response tf_existing-custom-skill-origin
