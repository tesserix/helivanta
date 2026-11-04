// Package contract defines the organization owner's data-only event interface.
package contract

// SubjectHospitalReferenceRequested asks the hospital owner to observe a canonical reference.
const SubjectHospitalReferenceRequested = "helivanta.in.organization.hospital_reference_requested.v1"

// SubjectDepartmentRevisionChanged identifies a retained organization structure revision.
const SubjectDepartmentRevisionChanged = "helivanta.in.organization.department_revision_changed.v1"

// SubjectDepartmentLinkObserved carries retained catalog evidence, never operating authority.
const SubjectDepartmentLinkObserved = "helivanta.in.organization.department_link_observed.v1"

// SubjectAssignmentDepartmentVerified answers whether a department is currently active in its hospital.
const SubjectAssignmentDepartmentVerified = "helivanta.in.organization.assignment_department_verified.v1"
