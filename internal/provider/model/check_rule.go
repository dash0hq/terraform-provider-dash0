package model

import "github.com/hashicorp/terraform-plugin-framework/types"

type CheckRuleResourceModel struct {
	Origin        types.String `tfsdk:"origin"`
	Dataset       types.String `tfsdk:"dataset"`
	CheckRuleYaml types.String `tfsdk:"check_rule_yaml"`
}
