package model

import "github.com/hashicorp/terraform-plugin-framework/types"

type RecordingRuleGroup struct {
	Origin                 types.String `tfsdk:"origin"`
	Dataset                types.String `tfsdk:"dataset"`
	RecordingRuleGroupYaml types.String `tfsdk:"recording_rule_group_yaml"`
}
