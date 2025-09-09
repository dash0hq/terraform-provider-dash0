package model

import "github.com/hashicorp/terraform-plugin-framework/types"

type SyntheticCheckResourceModel struct {
	Origin             types.String `tfsdk:"origin"`
	Dataset            types.String `tfsdk:"dataset"`
	SyntheticCheckYaml types.String `tfsdk:"synthetic_check_yaml"`
}
