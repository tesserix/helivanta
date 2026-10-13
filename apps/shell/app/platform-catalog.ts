export const platformGroups = [
  {
    name: "Patient care",
    description: "From the first appointment to the next stage of care.",
    color: "bg-teal-50 text-teal-900",
    modules: [
      {
        name: "Patient management",
        product: "Patient identity · MediCore",
        status: "Foundation available",
        description:
          "Patient registration, identifiers and demographic records. Consent and governed identity linking are planned extensions.",
      },
      {
        name: "Appointments & reception",
        product: "MediCore",
        status: "Planned",
        description:
          "Appointment slots, reception queues, check-in and coordinated booking across care teams.",
      },
      {
        name: "Outpatient & inpatient care",
        product: "MediCore",
        status: "Foundation available",
        description:
          "Outpatient visit workflows are available. Admissions, transfers, discharge and ward and bed management are planned.",
      },
      {
        name: "Clinical records & specialties",
        product: "MediCore",
        status: "Planned",
        description:
          "Clinical notes, nursing assessments, theatre checklists, oncology, chemotherapy and dietary care.",
      },
      {
        name: "Clinician workspace",
        product: "DoctorConnect",
        status: "Planned",
        description:
          "Care-team tasks, clinical review and coordinated handoffs through a dedicated clinician experience.",
      },
    ],
  },
  {
    name: "Diagnostics, medicines & supplies",
    description: "Connect clinical requests with the teams and resources behind them.",
    color: "bg-violet-50 text-violet-900",
    modules: [
      {
        name: "Pharmacy management",
        product: "PharmaConnect",
        status: "Foundation available",
        description:
          "Medication references and dispensing workflows. Batch and expiry control, pharmacy stock, sales and returns are planned.",
      },
      {
        name: "Laboratory management",
        product: "LabConnect",
        status: "Foundation available",
        description:
          "Laboratory orders and worklists. Specimen tracking, analyser integration and validated result release are planned.",
      },
      {
        name: "Imaging & radiology",
        product: "MediCore",
        status: "Planned",
        description: "Radiology requests, imaging workflow, reporting and DICOM/PACS integration.",
      },
      {
        name: "Inventory & procurement",
        product: "SupplyConnect",
        status: "Planned",
        description:
          "Suppliers, requisitions, purchase orders, goods receipts, central stores, stock transfers and approval workflows.",
      },
    ],
  },
  {
    name: "Hospital operations",
    description: "Give every operational team a clear place in the care network.",
    color: "bg-amber-50 text-amber-900",
    modules: [
      {
        name: "Staff & hospital management",
        product: "MediCore",
        status: "Available",
        description:
          "Hospital setup, hospital-scoped staff directories, employee profiles and manager or read-only access.",
      },
      {
        name: "Workforce & payroll",
        product: "MediCore",
        status: "Planned",
        description:
          "HR administration, attendance, leave, workforce planning and payroll workflows.",
      },
      {
        name: "Billing & insurance",
        product: "MediCore · PharmaConnect",
        status: "Planned",
        description:
          "Hospital and pharmacy billing, payments, refunds, insurer and TPA workflows, and attributable patient statements.",
      },
      {
        name: "Finance & accounts",
        product: "MediCore",
        status: "Planned",
        description:
          "Hospital accounts, receipts, grants, reconciliation and financial reporting with controlled ledger access.",
      },
      {
        name: "Assets & facilities",
        product: "FacilityConnect",
        status: "Planned",
        description: "Asset registers, equipment custody, maintenance and gate-pass workflows.",
      },
      {
        name: "Emergency & ambulance",
        product: "EmergencyConnect · MediCore",
        status: "Planned",
        description:
          "Dispatch, ambulance and crew tasks, pre-arrival handoffs and hospital casualty workflows.",
      },
      {
        name: "Home care",
        product: "CareConnect",
        status: "Planned",
        description:
          "Home-care plans, scheduled visits, observations and escalation back to the clinical team.",
      },
      {
        name: "Safety & quality",
        product: "SafetyConnect",
        status: "Planned",
        description:
          "Incident reporting, inspections, drills and corrective actions with restricted record access.",
      },
    ],
  },
  {
    name: "Relationships & administration",
    description: "Connect people and organisations while keeping access deliberate.",
    color: "bg-sky-50 text-sky-900",
    modules: [
      {
        name: "CRM & referrals",
        product: "CRM",
        status: "Planned",
        description:
          "Enquiries, contacts, referrals, booking requests and consent-aware outreach, separate from clinical-record access.",
      },
      {
        name: "Patient portal",
        product: "MediConnect",
        status: "Planned",
        description:
          "Patient and delegate access, self-service requests, communication preferences and released records.",
      },
      {
        name: "Reporting & administration",
        product: "AdminConnect",
        status: "Planned",
        description:
          "Governed reports, MIS, operational analytics, exports and support workflows scoped to authorised hospitals.",
      },
      {
        name: "Tenant access & branding",
        product: "Shared platform",
        status: "Foundation available",
        description:
          "Organisation access, roles, hospital permissions and custom logos and login themes. Self-service signup is still planned.",
      },
      {
        name: "Documents & integrations",
        product: "Shared platform",
        status: "Planned",
        description:
          "Controlled file access, malware scanning, notifications and integrations with clinical, payment and messaging systems.",
      },
      {
        name: "AI-assisted workflows",
        product: "Future platform capability",
        status: "Future",
        description:
          "Permission-aware knowledge retrieval and assistance, subject to separate privacy and clinical-safety validation.",
      },
    ],
  },
];
