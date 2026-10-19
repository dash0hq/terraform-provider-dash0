#!/bin/bash
# Auth tokens are organization-scoped, so the import ID has no dataset prefix.
# Use the auth token ID (a UUID) or its origin.
terraform import dash0_auth_token.ingest 5f1c4a52-7a6e-4c4e-9d0e-3f6a1b2c3d4e
