# Deployment events mark the point in time at which a service was deployed, and
# can be overlaid on charts as dashboard annotations. Sending them requires the
# provider's `otlp_url` attribute (or the DASH0_OTLP_URL environment variable).
action "dash0_deployment_event" "release" {
  config {
    service_name                = "checkout-api"
    service_version             = var.image_tag
    deployment_environment_name = "production"
    deployment_status           = "succeeded"

    vcs_repository_url    = "https://github.com/acme/checkout-api"
    vcs_ref_head_revision = var.git_sha
    vcs_ref_head_name     = "main"

    dataset = "production"
  }
}

# Attach the event to whatever resource actually represents the deployment. The
# action runs only when Terraform changed something, which is exactly when a
# deployment happened.
resource "kubernetes_deployment" "checkout_api" {
  # ... deployment configuration ...

  lifecycle {
    action_trigger {
      events  = [after_create, after_update]
      actions = [action.dash0_deployment_event.release]
    }
  }
}

# Bracketing a rollout takes two action instances, because one event carries one
# status. Note that Terraform has no after-failure event, so a failed apply emits
# the "started" marker and nothing else.
action "dash0_deployment_event" "started" {
  config {
    service_name                = "checkout-api"
    service_version             = var.image_tag
    deployment_environment_name = "production"
    deployment_status           = "started"
    dataset                     = "production"
  }
}

action "dash0_deployment_event" "succeeded" {
  config {
    service_name                = "checkout-api"
    service_version             = var.image_tag
    deployment_environment_name = "production"
    deployment_status           = "succeeded"
    dataset                     = "production"
  }
}

resource "kubernetes_deployment" "payments_api" {
  # ... deployment configuration ...

  lifecycle {
    action_trigger {
      events  = [before_create, before_update]
      actions = [action.dash0_deployment_event.started]
    }
    action_trigger {
      events  = [after_create, after_update]
      actions = [action.dash0_deployment_event.succeeded]
    }
  }
}

# One marker per service, without repeating the configuration.
action "dash0_deployment_event" "per_service" {
  for_each = toset(["checkout-api", "payments-api", "search-api"])

  config {
    service_name                = each.value
    service_version             = var.image_tag
    deployment_environment_name = "production"
    deployment_status           = "succeeded"
    dataset                     = "production"
  }
}
