import type { ModuleName } from "@/app/platform-catalog";

export type Lane = "management" | "administration" | "departments";
export type Flow = "care" | "stock" | "oversight";

export const teams = {
  governance: {
    label: "Governing body",
    detail: "Board, trustees or Rogi Kalyan Samiti",
    lane: "management",
  },
  leadership: {
    label: "Hospital leadership",
    detail: "CEO, Medical Superintendent",
    lane: "management",
  },
  heads: { label: "Department heads", detail: "HODs, Nursing Superintendent", lane: "management" },
  hr: { label: "HR & staff", detail: "Postings, rosters, credentials", lane: "administration" },
  finance: { label: "Finance", detail: "Budgets, billing, accounts", lane: "administration" },
  purchase: { label: "Purchase", detail: "Requests, orders, vendors", lane: "administration" },
  wards: { label: "Wards & OPD", detail: "MediCore", lane: "departments" },
  pharmacy: { label: "Pharmacy", detail: "PharmaConnect", lane: "departments" },
  lab: { label: "Laboratory", detail: "LabConnect", lane: "departments" },
  store: { label: "Central store", detail: "SupplyConnect", lane: "departments" },
  maintenance: { label: "Maintenance", detail: "FacilityConnect", lane: "departments" },
  emergency: { label: "Emergency", detail: "EmergencyConnect", lane: "departments" },
} as const satisfies Record<string, { label: string; detail: string; lane: Lane }>;

export type TeamId = keyof typeof teams;

export type Story = {
  id: string;
  title: string;
  summary: string;
  flow: Flow;
  steps: readonly { from: TeamId; to: TeamId; text: string }[];
  modules: readonly ModuleName[];
  note: string;
};

export const stories: readonly Story[] = [
  {
    id: "indent",
    title: "A ward runs short",
    summary: "Ward 4 needs a medicine its cupboard has run out of.",
    flow: "stock",
    steps: [
      { from: "wards", to: "pharmacy", text: "Ward 4 sends an indent to the pharmacy." },
      {
        from: "pharmacy",
        to: "wards",
        text: "The pharmacy issues what it holds, with batch and expiry, and flags the shortfall.",
      },
      { from: "pharmacy", to: "store", text: "The pharmacy asks the central store to replenish." },
      {
        from: "store",
        to: "pharmacy",
        text: "The store transfers stock, recording every batch it sends.",
      },
    ],
    modules: ["Pharmacy management", "Inventory & procurement"],
    note: "Dispensing with batch and expiry records is available as a foundation. Ward indents and store replenishment are planned.",
  },
  {
    id: "purchase",
    title: "A purchase needs sign-off",
    summary: "Stock falls below its reorder level, and the hospital's own rules decide who signs.",
    flow: "oversight",
    steps: [
      {
        from: "store",
        to: "purchase",
        text: "The central store raises a purchase request for items below reorder level.",
      },
      {
        from: "purchase",
        to: "heads",
        text: "The requesting department's head confirms the need.",
      },
      { from: "heads", to: "finance", text: "Finance checks the amount against the budget head." },
      {
        from: "finance",
        to: "leadership",
        text: "Above the hospital's limit, the Medical Superintendent or CEO approves.",
      },
      {
        from: "leadership",
        to: "governance",
        text: "The largest amounts go to the board or Rogi Kalyan Samiti.",
      },
    ],
    modules: ["Inventory & procurement", "Finance & accounts"],
    note: "Configurable approval chains are planned. Limits and approvers are always the hospital's own.",
  },
  {
    id: "emergency",
    title: "An emergency patient arrives",
    summary: "An ambulance is on its way, and casualty knows before the patient does.",
    flow: "care",
    steps: [
      {
        from: "emergency",
        to: "wards",
        text: "The crew's handoff reaches casualty before the ambulance arrives.",
      },
      {
        from: "wards",
        to: "lab",
        text: "The doctor orders urgent tests from the patient's visit.",
      },
      { from: "lab", to: "wards", text: "The laboratory releases results to the ordering doctor." },
      {
        from: "wards",
        to: "pharmacy",
        text: "Prescribed medicines reach the pharmacy without being written out again.",
      },
    ],
    modules: [
      "Emergency & ambulance",
      "Outpatient & inpatient care",
      "Laboratory management",
      "Pharmacy management",
    ],
    note: "Lab orders are available as a foundation. Ambulance handoffs and validated result release are planned.",
  },
  {
    id: "transfer",
    title: "A nurse moves to the ICU",
    summary: "Access follows the posting, not a password shared at the nurses' station.",
    flow: "oversight",
    steps: [
      { from: "hr", to: "heads", text: "HR records the nurse's posting to the ICU from Monday." },
      { from: "heads", to: "hr", text: "The ICU head approves the role the nurse will hold." },
      {
        from: "hr",
        to: "wards",
        text: "On Monday, Ward 4 access ends and ICU access begins.",
      },
    ],
    modules: ["Staff & hospital management", "Workforce & payroll"],
    note: "Hospital-scoped staff access is available. Access that changes with a posting is planned.",
  },
  {
    id: "overview",
    title: "Leaders see the whole hospital",
    summary: "Numbers travel up. Patient files stay with the teams caring for the patient.",
    flow: "oversight",
    steps: [
      {
        from: "wards",
        to: "leadership",
        text: "Wards report occupancy, admissions and discharges.",
      },
      {
        from: "pharmacy",
        to: "leadership",
        text: "The pharmacy reports stock-outs and items near expiry.",
      },
      {
        from: "maintenance",
        to: "leadership",
        text: "Maintenance reports equipment that is out of service.",
      },
      {
        from: "finance",
        to: "governance",
        text: "Finance reports collections and spend against budget.",
      },
    ],
    modules: ["Reporting & administration"],
    note: "Management reporting is planned.",
  },
];
