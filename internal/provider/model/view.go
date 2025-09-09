package model

import "github.com/hashicorp/terraform-plugin-framework/types"

type ViewResourceModel struct {
	Origin   types.String `tfsdk:"origin"`
	Dataset  types.String `tfsdk:"dataset"`
	ViewYaml types.String `tfsdk:"view_yaml"`
}
