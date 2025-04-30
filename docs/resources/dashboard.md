---
page_title: "dash0_dashboard Resource - terraform-provider-dash0"
subcategory: ""
description: |-
  Manages a Dash0 Dashboard (in Perses format).
---

# dash0_dashboard (Resource)

Manages a Dash0 Dashboard in Perses format. The dashboard definition is provided in YAML format and will be sent to the Dash0 API.

## Example Usage

```terraform
# Option 1: Inline YAML
resource "dash0_dashboard" "example_inline" {
  name        = "example-dashboard"
  description = "Example dashboard created via Terraform"
  dataset     = "production"  # Optional, defaults to "default" if not specified

  dashboard_definition_yaml = <<-EOT
    kind: Dashboard
    metadata:
      name: example-dashboard
    spec:
      panels:
        - kind: Panel
          spec:
            display:
              name: Sample Panel
            plugin:
              kind: TimeSeriesChart
              spec:
                queries:
                  - kind: PrometheusTimeSeriesQuery
                    spec:
                      query: "up"
  EOT
}

# Option 2: Load YAML from a file
resource "dash0_dashboard" "example_file" {
  name        = "example-dashboard"
  description = "Example dashboard created via Terraform"
  dataset     = "default"  # Optional, defaults to "default" if not specified

  # Load the dashboard definition from a local YAML file
  dashboard_definition_yaml = file("${path.module}/dashboards/example-dashboard.yaml")
}
```

## Argument Reference

* `dataset` - (Optional) The dataset for which the dashboard is created. Defaults to "default" if not specified.
* `dashboard_definition_yaml` - (Required) The dashboard definition in YAML format (Perses Dashboard format).

## Attribute Reference

In addition to all arguments above, the following attributes are exported:

* `id` - The ID of the dashboard.

## Import

Dashboards can be imported using the dashboard name, e.g.,

```shell
terraform import dash0_dashboard.example example-dashboard
```
