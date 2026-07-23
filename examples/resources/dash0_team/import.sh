#!/bin/bash
# Teams are organization-scoped, so the import ID is a bare identifier — no
# dataset prefix. Both the provider-generated origin (tf_-prefixed) and the
# raw team id (server-assigned UUID) are accepted; the example uses an origin.
terraform import dash0_team.backend tf_existing-team-origin
