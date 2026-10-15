#!/bin/bash
# Datasets are organization-scoped, so the import ID is the dataset slug
# (the `dash0.com/id` label) — no dataset prefix.
terraform import dash0_dataset.staging staging
