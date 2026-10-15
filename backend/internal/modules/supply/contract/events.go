// Package contract declares SupplyConnect's minimized published references.
package contract

const SubjectPartyUpdated = "helivanta.in.supply.party_updated.v1"

type PartyUpdatedData struct {
	PartyID string `json:"party_id"`
	Version int64  `json:"version"`
}

const SubjectPartyAccessChanged = "helivanta.in.supply.party_access_changed.v1"

type PartyAccessChangedData struct {
	PartyID   string `json:"party_id"`
	AccessID  string `json:"access_id"`
	Operation string `json:"operation"`
}
