package supply

import (
	"reflect"
	"strings"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/countryprofile"
	"gorm.io/gorm"
)

type contactRow struct {
	ContactFields `gorm:"embedded"`
	TenantID      uuid.UUID
	PartyID       uuid.UUID
	Position      int
}

func (contactRow) TableName() string { return "supply_party_contacts" }

type addressRow struct {
	AddressFields `gorm:"embedded"`
	TenantID      uuid.UUID
	PartyID       uuid.UUID
	Position      int
}

func (addressRow) TableName() string { return "supply_party_addresses" }

type taxRow struct {
	TaxRegistrationFields `gorm:"embedded"`
	TenantID              uuid.UUID
	PartyID               uuid.UUID
	Position              int
	CountryCode           string
}

func (taxRow) TableName() string { return "supply_party_tax_registrations" }

func validComponentID(id uuid.UUID, seen map[uuid.UUID]bool) bool {
	if id.Version() != 7 || id.Variant() != uuid.RFC4122 || seen[id] {
		return false
	}
	seen[id] = true
	return true
}
func (in *PartyFields) normalizeParts() bool {
	if in.Contacts == nil || in.Addresses == nil || in.TaxRegistrations == nil {
		return false
	}
	seen := map[uuid.UUID]bool{}
	for i := range in.Contacts {
		v := &in.Contacts[i]
		v.Name = strings.TrimSpace(v.Name)
		v.Email = strings.TrimSpace(v.Email)
		v.Phone = strings.TrimSpace(v.Phone)
		v.AlternatePhone = strings.TrimSpace(v.AlternatePhone)
		if !validComponentID(v.ID, seen) || !validContactFieldsPatterns(*v) {
			return false
		}
	}
	seen = map[uuid.UUID]bool{}
	for i := range in.Addresses {
		v := &in.Addresses[i]
		for _, s := range []*string{&v.Line1, &v.Line2, &v.City, &v.Region, &v.PostalCode} {
			*s = strings.TrimSpace(*s)
		}
		if !validComponentID(v.ID, seen) || (v.Kind == "registered" && v.CountryCode != in.CountryCode) {
			return false
		}
	}
	seen = map[uuid.UUID]bool{}
	identities := map[string]bool{}
	for i := range in.TaxRegistrations {
		v := &in.TaxRegistrations[i]
		v.Number = strings.ToUpper(strings.TrimSpace(v.Number))
		v.Jurisdiction = strings.TrimSpace(v.Jurisdiction)
		key := v.Kind + ":" + v.Number
		if !validComponentID(v.ID, seen) || identities[key] || !countryprofile.ValidTaxIdentifierKind(in.CountryCode, v.Kind) {
			return false
		}
		identities[key] = true
	}
	return true
}
func loadPartyParts(tx *gorm.DB, p *party) error {
	var contacts []contactRow
	var addresses []addressRow
	var registrations []taxRow
	if e := tx.Where("tenant_id=? AND party_id=?", p.TenantID, p.ID).Order("position").Find(&contacts).Error; e != nil {
		return e
	}
	if e := tx.Where("tenant_id=? AND party_id=?", p.TenantID, p.ID).Order("position").Find(&addresses).Error; e != nil {
		return e
	}
	if e := tx.Where("tenant_id=? AND party_id=?", p.TenantID, p.ID).Order("position").Find(&registrations).Error; e != nil {
		return e
	}
	p.Contacts = make([]ContactFields, 0, len(contacts))
	p.Addresses = make([]AddressFields, 0, len(addresses))
	p.TaxRegistrations = make([]TaxRegistrationFields, 0, len(registrations))
	for _, v := range contacts {
		p.Contacts = append(p.Contacts, v.ContactFields)
	}
	for _, v := range addresses {
		p.Addresses = append(p.Addresses, v.AddressFields)
	}
	for _, v := range registrations {
		p.TaxRegistrations = append(p.TaxRegistrations, v.TaxRegistrationFields)
	}
	return nil
}

func (r contactRow) componentID() uuid.UUID { return r.ID }
func (r addressRow) componentID() uuid.UUID { return r.ID }
func (r taxRow) componentID() uuid.UUID     { return r.ID }
func contactRows(p *party) []contactRow {
	rows := make([]contactRow, len(p.Contacts))
	for i, v := range p.Contacts {
		rows[i] = contactRow{ContactFields: v, TenantID: p.TenantID, PartyID: p.ID, Position: i}
	}
	return rows
}
func addressRows(p *party) []addressRow {
	rows := make([]addressRow, len(p.Addresses))
	for i, v := range p.Addresses {
		rows[i] = addressRow{AddressFields: v, TenantID: p.TenantID, PartyID: p.ID, Position: i}
	}
	return rows
}
func taxRows(p *party) []taxRow {
	rows := make([]taxRow, len(p.TaxRegistrations))
	for i, v := range p.TaxRegistrations {
		rows[i] = taxRow{TaxRegistrationFields: v, TenantID: p.TenantID, PartyID: p.ID, Position: i, CountryCode: p.CountryCode}
	}
	return rows
}
func syncPartyRows[T interface{ componentID() uuid.UUID }](tx *gorm.DB, p *party, table string, previous, desired []T) error {
	old := make(map[uuid.UUID]T, len(previous))
	current := make(map[uuid.UUID]bool, len(desired))
	for _, row := range previous {
		old[row.componentID()] = row
	}
	for _, row := range desired {
		current[row.componentID()] = true
	}
	for _, row := range previous {
		if !current[row.componentID()] {
			if e := tx.Table(table).Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, p.ID, row.componentID()).Delete(nil).Error; e != nil {
				return e
			}
		}
	}
	for _, row := range desired {
		prior, exists := old[row.componentID()]
		if exists && reflect.DeepEqual(prior, row) {
			continue
		}
		if exists {
			if e := tx.Table(table).Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, p.ID, row.componentID()).Select("*").Omit("id", "tenant_id", "party_id").Updates(&row).Error; e != nil {
				return e
			}
		} else if e := tx.Table(table).Create(&row).Error; e != nil {
			return e
		}
	}
	return nil
}
func storePartyParts(tx *gorm.DB, p *party, before PartyFields) error {
	previous := *p
	previous.PartyFields = before
	if e := syncPartyRows(tx, p, "supply_party_contacts", contactRows(&previous), contactRows(p)); e != nil {
		return e
	}
	if e := syncPartyRows(tx, p, "supply_party_addresses", addressRows(&previous), addressRows(p)); e != nil {
		return e
	}
	return syncPartyRows(tx, p, "supply_party_tax_registrations", taxRows(&previous), taxRows(p))
}
