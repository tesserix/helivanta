package iam

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
)

func TestGovernanceAssignmentProofNeverAdoptsAnotherSource(t *testing.T) {
	hospital := uuid.Must(uuid.NewV7())
	p := authn.Principal{TenantID: uuid.NewString(), Subject: "synthetic-access-reviewer", AuthTime: time.Now().Add(-time.Minute), IdleDeadline: time.Now().Add(time.Hour)}
	source := authz.GovernanceSourceEvidence{Kind: authz.GovernanceGrantSource, GrantID: uuid.Must(uuid.NewV7()), Version: 1, MembershipEpoch: 2, TemplateKey: "iam.scoped_access_reviewer.v1", Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: "hospital", ID: &hospital}, ExpiresAt: time.Now().Add(time.Hour)}
	proof, err := assignmentProof(p, hospital, authz.GovernanceAssignmentManage, source)
	require.NoError(t, err)
	require.True(t, sameAssignmentSource(proof, proof))
	for name, mutate := range map[string]func(*assignmentGovernanceProof){
		"tenant":        func(v *assignmentGovernanceProof) { v.TenantID = uuid.NewString() },
		"hospital":      func(v *assignmentGovernanceProof) { v.HospitalID = uuid.Must(uuid.NewV7()) },
		"actor":         func(v *assignmentGovernanceProof) { v.ActorSubject = "synthetic-other-reviewer" },
		"capability":    func(v *assignmentGovernanceProof) { v.Capability = authz.GovernanceAssignmentReview },
		"membership":    func(v *assignmentGovernanceProof) { v.MembershipEpoch++ },
		"grant":         func(v *assignmentGovernanceProof) { id := uuid.Must(uuid.NewV7()); v.GrantID = &id },
		"grant version": func(v *assignmentGovernanceProof) { version := int64(2); v.GrantVersion = &version },
		"owner alias":   func(v *assignmentGovernanceProof) { v.Kind = authz.GovernanceOwnerSource },
		"missing grant": func(v *assignmentGovernanceProof) { v.GrantID = nil },
	} {
		t.Run(name, func(t *testing.T) {
			changed := proof
			mutate(&changed)
			require.False(t, sameAssignmentSource(proof, changed))
		})
	}
	p.AuthTime = time.Now()
	p.IdleDeadline = p.IdleDeadline.Add(time.Minute)
	refreshed, err := assignmentProof(p, hospital, authz.GovernanceAssignmentManage, source)
	require.NoError(t, err)
	require.True(t, sameAssignmentSource(proof, refreshed), "fresh authentication cannot silently change original source identity")
	source = authz.GovernanceSourceEvidence{Kind: authz.GovernanceOwnerSource, OwnerMembershipID: uuid.Must(uuid.NewV7()), MembershipEpoch: 2, Scope: source.Scope, ExpiresAt: source.ExpiresAt}
	owner, err := assignmentProof(p, hospital, authz.GovernanceAssignmentManage, source)
	require.NoError(t, err)
	require.Nil(t, owner.GrantID)
	require.True(t, sameAssignmentSource(owner, owner))
	other := owner
	id := uuid.Must(uuid.NewV7())
	other.OwnerMembershipID = &id
	require.False(t, sameAssignmentSource(owner, other))
}

func TestGovernanceAssignmentCapabilitiesCannotAliasOtherScopes(t *testing.T) {
	for _, capability := range []authz.GovernanceCapability{authz.GovernanceAssignmentManage, authz.GovernanceAssignmentReview} {
		require.Equal(t, "iam.scoped_access_reviewer.v1", governanceSourceTemplate(capability, "hospital"))
		for _, kind := range []string{"", "tenant", "pharmacy", "supplier", "department"} {
			require.Empty(t, governanceSourceTemplate(capability, kind))
		}
	}
}

func TestGovernanceAssignmentResultRequiresItsOwnProtocolAndExplicitVerdict(t *testing.T) {
	data := workforcecontract.AssignmentGovernanceScopeVerifiedData{ProposalID: uuid.Must(uuid.NewV7()), RequestID: uuid.Must(uuid.NewV7()), HospitalID: uuid.Must(uuid.NewV7()), Eligible: true}
	raw, err := json.Marshal(data)
	require.NoError(t, err)
	valid := events.Event{TenantID: uuid.NewString(), Type: workforcecontract.SubjectAssignmentGovernanceScopeVerified, Version: 1, Data: raw}
	result, err := assignmentGovernanceResult(valid)
	require.NoError(t, err)
	require.Equal(t, data, result)
	for name, mutate := range map[string]func(*events.Event){
		"legacy protocol":   func(e *events.Event) { e.Type = workforcecontract.SubjectAssignmentScopeVerified },
		"case alias":        func(e *events.Event) { e.Data = []byte(strings.Replace(string(raw), "proposal_id", "PROPOSAL_ID", 1)) },
		"duplicate verdict": func(e *events.Event) { e.Data = []byte(strings.TrimSuffix(string(raw), "}") + `,"eligible":false}`) },
		"version":           func(e *events.Event) { e.Version = 2 },
		"destination":       func(e *events.Event) { e.DestinationTenantID = uuid.NewString() },
		"tenant":            func(e *events.Event) { e.TenantID = "tenant-from-client" },
		"missing verdict":   func(e *events.Event) { e.Data = []byte(strings.Replace(string(raw), `,"eligible":true`, "", 1)) },
		"null verdict": func(e *events.Event) {
			e.Data = []byte(strings.Replace(string(raw), `"eligible":true`, `"eligible":null`, 1))
		},
		"bearer source": func(e *events.Event) {
			e.Data = []byte(strings.TrimSuffix(string(raw), "}") + `,"authority":{"kind":"owner"}}`)
		},
		"trailing JSON": func(e *events.Event) { e.Data = append(append([]byte{}, raw...), []byte(`{}`)...) },
		"old request": func(e *events.Event) {
			changed := data
			changed.RequestID = uuid.New()
			e.Data, _ = json.Marshal(changed)
		},
		"nil hospital": func(e *events.Event) {
			changed := data
			changed.HospitalID = uuid.Nil
			e.Data, _ = json.Marshal(changed)
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := valid
			mutate(&changed)
			_, err := assignmentGovernanceResult(changed)
			require.Error(t, err)
		})
	}
	data.Eligible = false
	valid.Data, err = json.Marshal(data)
	require.NoError(t, err)
	result, err = assignmentGovernanceResult(valid)
	require.NoError(t, err)
	require.False(t, result.Eligible, "an explicit owner refusal remains a valid immutable answer")
}

func TestGovernanceAssignmentCatalogRejectsUnauthenticatedRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	routes := platform.NewRouter(router.Group("/v1/iam"), &CurrentAuthorization{})
	registerGovernanceAssignments(routes, platform.Deps{})
	request := httptest.NewRequest(http.MethodGet, "/v1/iam/hospitals/11111111-1111-4111-8111-111111111111/assignment-templates", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusUnauthorized, response.Code, response.Body.String())
	require.NotContains(t, response.Body.String(), "workforce.hr_intake.v1")
}
