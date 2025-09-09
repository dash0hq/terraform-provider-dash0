package model

import "github.com/hashicorp/terraform-plugin-framework/types"

type CheckRule struct {
	Origin        types.String `tfsdk:"origin"`
	Dataset       types.String `tfsdk:"dataset"`
	CheckRuleYaml types.String `tfsdk:"check_rule_yaml"`
}
