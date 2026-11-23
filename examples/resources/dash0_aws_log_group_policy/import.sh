#!/bin/bash
# The import ID is the id (UUID) of the AWS integration: there is at most one
# log-group policy per integration, and no dataset prefix.
terraform import dash0_aws_log_group_policy.prod 27755ed0-0009-422d-8c11-19c6dd72df98
