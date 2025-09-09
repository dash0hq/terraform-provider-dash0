package model

import "github.com/hashicorp/terraform-plugin-framework/types"

type Dashboard struct {
	Origin        types.String `tfsdk:"origin"`
	Dataset       types.String `tfsdk:"dataset"`
	DashboardYaml types.String `tfsdk:"dashboard_yaml"`
}
