package workforce

import (
	"encoding/json"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func readEnrollment(tx *gorm.DB, tenant string, hospital, id uuid.UUID, row staff) (enrollment, error) {
	result := enrollment{Staff: row, Version: row.Version, Profile: enrollmentProfile{EngagementType: "employee"}, Addresses: []enrollmentAddress{}, EmergencyContacts: []enrollmentContact{}, Credentials: []enrollmentCredential{}, Qualifications: []enrollmentQualification{}, Experiences: []enrollmentExperience{}}
	var data []byte
	err := tx.Raw(`WITH scope AS (SELECT ?::uuid AS tenant_id, ?::uuid AS hospital_id, ?::uuid AS staff_id)
 SELECT jsonb_build_object(
 'profile', (SELECT to_jsonb(r) FROM workforce_enrollment_profile r JOIN scope USING (tenant_id,hospital_id,staff_id)),
 'addresses', COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM workforce_enrollment_addresses r JOIN scope USING (tenant_id,hospital_id,staff_id)), '[]'::jsonb),
 'emergency_contacts', COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM workforce_enrollment_emergency_contacts r JOIN scope USING (tenant_id,hospital_id,staff_id)), '[]'::jsonb),
 'credentials', COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM workforce_enrollment_credentials r JOIN scope USING (tenant_id,hospital_id,staff_id)), '[]'::jsonb),
 'qualifications', COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM workforce_enrollment_qualifications r JOIN scope USING (tenant_id,hospital_id,staff_id)), '[]'::jsonb),
 'experiences', COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM workforce_enrollment_experiences r JOIN scope USING (tenant_id,hospital_id,staff_id)), '[]'::jsonb))`, tenant, hospital, id).Row().Scan(&data)
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(data, &result)
	return result, err
}
