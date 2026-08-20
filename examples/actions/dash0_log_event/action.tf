# The general-purpose counterpart to dash0_deployment_event: an arbitrary log
# record with a free-form event name and attributes, mirroring `dash0 logs send`.
# Requires the provider's `otlp_url` attribute (or DASH0_OTLP_URL).
action "dash0_log_event" "migration_finished" {
  config {
    body            = "Database migration ${var.migration_version} applied"
    event_name      = "acme.migration"
    severity_number = 9
    severity_text   = "INFO"

    # Attributes describing the entity the record is about.
    resource_attributes = {
      "service.name"                = "checkout-api"
      "deployment.environment.name" = "production"
    }

    # Attributes describing this individual event.
    log_attributes = {
      "migration.version" = var.migration_version
      "migration.applied" = "true"
    }

    dataset = "production"
  }
}

resource "null_resource" "database_migration" {
  # ... migration configuration ...

  lifecycle {
    action_trigger {
      events  = [after_create, after_update]
      actions = [action.dash0_log_event.migration_finished]
    }
  }
}

# Correlate the record with an existing trace by supplying both halves of the
# trace context. Setting only one of the two is a configuration error.
action "dash0_log_event" "correlated" {
  config {
    body     = "Request processed"
    trace_id = "0af7651916cd43dd8448eb211c80319c"
    span_id  = "b7ad6b7169203331"
    dataset  = "production"
  }
}

# Treat a delivery failure as fatal. The default is false, so that a transient
# ingestion problem does not fail an apply.
action "dash0_log_event" "audit_record" {
  config {
    body            = "Terraform applied infrastructure changes"
    event_name      = "acme.audit"
    severity_number = 9
    dataset         = "production"
    fail_on_error   = true
  }
}
