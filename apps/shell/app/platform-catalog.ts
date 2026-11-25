export const platformGroups = [
  {
    name: "Patient care",
    description: "From the first appointment to the next stage of care.",
    color: "bg-accent text-accent-foreground",
    modules: [
      {
        name: "Patient management",
        product: "Patient identity · MediCore",
        status: "Planned",
        description:
          "Patient registration, identifiers and demographic records, with consent and governed identity linking.",
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
        status: "Planned",
        description:
          "Outpatient visits, admissions, transfers, discharge, and ward and bed management.",
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
    color: "bg-(--hms-violet-tint) text-(--hms-violet)",
    modules: [
      {
        name: "Pharmacy management",
        product: "PharmaConnect",
        status: "Foundation available",
        description:
          "Pharmacy setup, pharmacy staff, low-stock alerts and stocktakes. Medication references, dispensing, batch and expiry control, the stock register, counter sales, returns, supplier payables and reports are planned.",
        evidence: [
          "/pharmacy/setup",
          "/pharmacy/staff",
          "/pharmacy/low-stock",
          "/pharmacy/working-stocktakes",
        ],
      },
      {
        name: "Laboratory management",
        product: "LabConnect",
        status: "Planned",
        description:
          "Laboratory orders and worklists, specimen tracking, analyser integration and validated result release.",
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
    color: "bg-(--hms-amber-tint) text-(--hms-amber)",
    modules: [
      {
        name: "Staff & hospital management",
        product: "MediCore",
        status: "Available",
        description:
          "Hospital setup, hospital-scoped staff directories, employee profiles and manager or read-only access.",
        evidence: ["/workforce", "/workforce/setup", "/workforce/access"],
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
    color: "bg-(--hms-sky-tint) text-(--hms-sky)",
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
        status: "Available",
        description:
          "Self-service organisation signup, organisation access, roles, hospital permissions, and custom logos and login themes.",
        evidence: ["/settings/branding", "/workforce/access"],
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
] as const;

type CatalogueModule = (typeof platformGroups)[number]["modules"][number];
export type ModuleName = CatalogueModule["name"];
export type ModuleStatus = CatalogueModule["status"];

const statuses = new Map<string, ModuleStatus>(
  platformGroups.flatMap((group) =>
    group.modules.map((module) => [module.name, module.status] as const),
  ),
);

export function moduleStatus(name: ModuleName): ModuleStatus {
  const status = statuses.get(name);
  if (!status) throw new Error(`module missing from the platform catalogue: ${name}`);
  return status;
}

export const isAvailable = (status: ModuleStatus) =>
  status === "Available" || status === "Foundation available";

// The home page lists only what a hospital can use today, and drops a group
// with nothing in it; the roadmap renders platformGroups whole (#1837 D3).
export type CatalogueGroup = {
  name: string;
  description: string;
  color: string;
  modules: readonly CatalogueModule[];
};

export const availableGroups: readonly CatalogueGroup[] = (
  platformGroups as readonly CatalogueGroup[]
)
  .map((group) => ({ ...group, modules: group.modules.filter((m) => isAvailable(m.status)) }))
  .filter((group) => group.modules.length > 0);

type ReleaseManifest = {
  capabilities: readonly { key: string; eligible: boolean; dependencies: readonly string[] }[];
  pages: readonly { path: string; capability: string }[];
};

// Each offered row names the shell pages behind it (#1839 D3). A page backs a
// row only when its capability, and every capability it depends on, is
// eligible in this release; baseline always is. Returns "<module>: <path>"
// for each row or path that fails, so CI names what to fix.
type EvidencedModule = { name: string; status: ModuleStatus; evidence?: readonly string[] };

export function unbackedRows(
  groups: readonly { modules: readonly EvidencedModule[] }[],
  release: ReleaseManifest,
): string[] {
  const capabilities = new Map(release.capabilities.map((c) => [c.key, c]));
  // The path guards against a dependency cycle; a capability reached twice
  // through different dependencies is not a cycle.
  const eligible = (key: string, path: readonly string[] = []): boolean => {
    if (key === "baseline") return true;
    const capability = capabilities.get(key);
    if (!capability?.eligible || path.includes(key)) return false;
    return capability.dependencies.every((dependency) => eligible(dependency, [...path, key]));
  };
  const pages = new Map(release.pages.map((page) => [page.path, page.capability]));
  return groups.flatMap((group) =>
    group.modules
      .filter((m) => isAvailable(m.status))
      .flatMap((m) => {
        const evidence = m.evidence ?? [];
        if (evidence.length === 0) return [`${m.name}: no evidence`];
        return evidence
          .filter((path) => {
            const capability = pages.get(path);
            return capability === undefined || !eligible(capability);
          })
          .map((path) => `${m.name}: ${path}`);
      }),
  );
}
