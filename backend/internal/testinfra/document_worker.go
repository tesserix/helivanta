package testinfra

import (
	"net"
	"net/url"
	"testing"
)

// DocumentWorkerDSN connects the dedicated fixture identity to an existing test database.
func DocumentWorkerDSN(t *testing.T, database string) string {
	t.Helper()
	host, port := sharedPostgres(t)
	u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(host, port), Path: "/" + database, User: url.UserPassword("helivanta_document_worker", "helivanta_document_worker")}
	q := url.Values{"sslmode": {"disable"}}
	u.RawQuery = q.Encode()
	return u.String()
}

// SupplyEvidenceWorkerDSN connects only the non-clinical evidence processing identity.
func SupplyEvidenceWorkerDSN(t *testing.T, database string) string {
	t.Helper()
	host, port := sharedPostgres(t)
	u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(host, port), Path: "/" + database, User: url.UserPassword("helivanta_supply_evidence_worker", "helivanta_supply_evidence_worker")}
	u.RawQuery = url.Values{"sslmode": {"disable"}}.Encode()
	return u.String()
}

// EmergencyWorkerDSN connects only the scoped dispatch-workflow fixture identity.
func EmergencyWorkerDSN(t *testing.T, database string) string {
	t.Helper()
	host, port := sharedPostgres(t)
	u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(host, port), Path: "/" + database, User: url.UserPassword("helivanta_emergency_worker", "helivanta_emergency_worker")}
	u.RawQuery = url.Values{"sslmode": {"disable"}}.Encode()
	return u.String()
}

// PrivacyWorkerDSN connects the restricted deadline-metadata fixture identity.
func PrivacyWorkerDSN(t *testing.T, database string) string {
	t.Helper()
	host, port := sharedPostgres(t)
	u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(host, port), Path: "/" + database, User: url.UserPassword("helivanta_privacy_worker", "helivanta_privacy_worker")}
	u.RawQuery = url.Values{"sslmode": {"disable"}}.Encode()
	return u.String()
}

// PractitionerEvidenceWorkerDSN connects only the scoped professional-evidence fixture identity.
func PractitionerEvidenceWorkerDSN(t *testing.T, database string) string {
	t.Helper()
	host, port := sharedPostgres(t)
	u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(host, port), Path: "/" + database, User: url.UserPassword("helivanta_practitioner_evidence_worker", "helivanta_practitioner_evidence_worker")}
	u.RawQuery = url.Values{"sslmode": {"disable"}}.Encode()
	return u.String()
}

// DrugStagerDSN connects only the drug master release-staging identity (#476 D3).
func DrugStagerDSN(t *testing.T, database string) string {
	t.Helper()
	host, port := sharedPostgres(t)
	u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(host, port), Path: "/" + database, User: url.UserPassword("helivanta_drug_stager", "helivanta_drug_stager")}
	u.RawQuery = url.Values{"sslmode": {"disable"}}.Encode()
	return u.String()
}

// DrugApproverDSN connects only the drug master release-decision identity (#476 D3).
func DrugApproverDSN(t *testing.T, database string) string {
	t.Helper()
	host, port := sharedPostgres(t)
	u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(host, port), Path: "/" + database, User: url.UserPassword("helivanta_drug_approver", "helivanta_drug_approver")}
	u.RawQuery = url.Values{"sslmode": {"disable"}}.Encode()
	return u.String()
}

// PharmacyEvidenceWorkerDSN connects only the scoped prescription-image fixture identity.
func PharmacyEvidenceWorkerDSN(t *testing.T, database string) string {
	t.Helper()
	host, port := sharedPostgres(t)
	u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(host, port), Path: "/" + database, User: url.UserPassword("helivanta_pharmacy_evidence_worker", "helivanta_pharmacy_evidence_worker")}
	u.RawQuery = url.Values{"sslmode": {"disable"}}.Encode()
	return u.String()
}
