import { randomUUID } from "node:crypto";

export function createHMSFixtures() {
  const tenants = ["YES HOSPITAL", "SYNTHETIC ISOLATION TENANT"].map((name) => ({
    id: randomUUID(),
    name,
    environment: "test",
  }));
  const hospitals = tenants.flatMap((tenant, index) =>
    Array.from({ length: index === 0 ? 2 : 1 }, (_, i) => ({
      id: randomUUID(),
      tenant_id: tenant.id,
      name: `Synthetic Hospital ${index + 1}-${i + 1}`,
    })),
  );
  const staff = [];
  const staffAssignments = [];
  const patients = [];
  const registrations = [];
  for (const tenant of tenants) {
    const tenantHospitals = hospitals.filter((h) => h.tenant_id === tenant.id);
    for (const job of ["doctor", "nurse", "pharmacist", "lab_technician", "receptionist"]) {
      const sequence = staff.length + 1;
      const person = {
        id: randomUUID(),
        tenant_id: tenant.id,
        employee_code: `SYN-EMP-${sequence}`,
        given_name: "Synthetic",
        family_name: `Staff ${sequence}`,
        email: `synthetic.staff.${sequence}@example.invalid`,
        phone: null,
        identity_subject: null,
        employment_status: sequence % 5 === 0 ? "inactive" : "active",
      };
      staff.push(person);
      // Employment at both hospitals must not imply access to either one.
      for (const hospital of tenantHospitals) {
        staffAssignments.push({
          tenant_id: tenant.id,
          hospital_id: hospital.id,
          staff_id: person.id,
          job_category: job,
        });
      }
    }
    for (const hospital of tenantHospitals) {
      for (let i = 0; i < 5; i++) {
        const sequence = patients.length + 1;
        const patient = {
          id: randomUUID(),
          tenant_id: tenant.id,
          given_name: "Synthetic",
          family_name: `Patient ${sequence}`,
          dob: `${1980 + i}-01-01`,
          dob_estimated: true,
          mobile: null,
          address: null,
        };
        patients.push(patient);
        registrations.push({
          tenant_id: tenant.id,
          hospital_id: hospital.id,
          patient_id: patient.id,
          mrn: `SYN-MRN-${sequence}`,
        });
      }
    }
  }
  return {
    fixture_version: 1,
    classification: "synthetic",
    source: "generated; no legacy records imported",
    tenants,
    hospitals,
    staff,
    staff_assignments: staffAssignments,
    patients,
    patient_registrations: registrations,
    access_grants: [],
    sharing_grants: [],
  };
}
