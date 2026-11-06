package iam

import (
	_ "embed"
	"github.com/tesserix/helivanta/pkg/authz"
)

const PermPartnerGroupMemberRead authz.Permission = "iam.partner_group.members.read"
const PermPartnerGroupMemberPrepare authz.Permission = "iam.partner_group.members.prepare"
const PermPartnerGroupMemberReview authz.Permission = "iam.partner_group.members.review"

//go:embed assignment_partner_membership_templates_generated.sql
var partnerMembershipTemplatesSchema string
