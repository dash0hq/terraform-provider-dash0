#!/bin/bash
# The import ID is the id (UUID) of the AWS integration: there is one scrape
# configuration per integration, and no dataset prefix.
terraform import dash0_aws_metric_scrape_config.prod 27755ed0-0009-422d-8c11-19c6dd72df98
