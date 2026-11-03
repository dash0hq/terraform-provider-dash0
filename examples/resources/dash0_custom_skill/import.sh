#!/bin/bash
# A skill created by Terraform or the Dash0 CLI is imported by its origin;
# a skill created in the Dash0 UI has no origin and is imported by its id.
terraform import dash0_custom_skill.name "{{ id_or_origin }}"
