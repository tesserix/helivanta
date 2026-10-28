import type { ModuleName } from "@/app/platform-catalog";

export const sources = {
  wards: { label: "Wards", detail: "Patient ready to go home" },
  pharmacy: { label: "Pharmacy", detail: "Stock running down" },
  billing: { label: "Billing desk", detail: "Insured bill closed" },
  nursing: { label: "Nursing office", detail: "Nurse calls in sick" },
  finance: { label: "Finance", detail: "Month-end close" },
} as const satisfies Record<string, { label: string; detail: string }>;

export const agents = {
  notes: { label: "Clinical notes agent", detail: "Drafts from the visit record" },
  stock: { label: "Stock forecasting agent", detail: "Reads usage and lead times" },
  claims: { label: "Claims agent", detail: "Assembles codes and documents" },
  rosters: { label: "Roster agent", detail: "Checks skills and rest rules" },
  audit: { label: "Audit review agent", detail: "Reads the trail, changes nothing" },
} as const satisfies Record<string, { label: string; detail: string }>;

export type SourceId = keyof typeof sources;
export type AgentId = keyof typeof agents;
export type NodeId = SourceId | AgentId | "supervisor" | "person" | "record";

export type Scenario = {
  id: string;
  title: string;
  summary: string;
  source: SourceId;
  agent: AgentId;
  approver: { name: string; role: string };
  outcome: string;
  narration: readonly [event: string, routing: string, draft: string, approval: string];
  modules: readonly ModuleName[];
};

export const scenarios: readonly Scenario[] = [
  {
    id: "discharge",
    title: "Discharge summary",
    summary: "A patient on Ward 4 is ready to go home and needs a discharge summary.",
    source: "wards",
    agent: "notes",
    approver: { name: "Dr. Mehta", role: "Treating doctor" },
    outcome: "Summary signed",
    narration: [
      "Ward 4 marks a patient ready for discharge, and the supervisor agent picks up the event.",
      "The supervisor sends it to the clinical notes agent, with access to this patient's visit only.",
      "The agent drafts a summary from the notes, results and medicines, citing each source, and hands it to the treating doctor.",
      "Dr. Mehta corrects the follow-up date and signs. The summary is filed as AI-assisted, with the edits kept.",
    ],
    modules: ["AI-assisted workflows", "Clinical records & specialties"],
  },
  {
    id: "reorder",
    title: "Medicine reorder",
    summary: "An infusion is running down faster than usual this week.",
    source: "pharmacy",
    agent: "stock",
    approver: { name: "Imran", role: "Pharmacy manager" },
    outcome: "Purchase request raised",
    narration: [
      "Daily use crosses the pharmacy's reorder level, and the supervisor agent picks it up.",
      "The supervisor asks the stock forecasting agent, which reads usage, open orders and supplier lead times.",
      "The agent drafts a purchase request for 30 days' cover and shows the forecast behind the quantity.",
      "Imran lowers the quantity and approves. Nothing is ordered without that approval, and the request is marked AI-assisted.",
    ],
    modules: ["AI-assisted workflows", "Inventory & procurement"],
  },
  {
    id: "claim",
    title: "Insurance claim",
    summary: "An insured patient's bill closes, and the insurer needs a complete claim.",
    source: "billing",
    agent: "claims",
    approver: { name: "Priya", role: "Insurance desk" },
    outcome: "Claim submitted",
    narration: [
      "The billing desk closes an insured patient's bill, and the supervisor agent picks up the event.",
      "The supervisor sends it to the claims agent, which sees the bill, the policy and the discharge summary.",
      "The agent assembles the claim, checks it against the insurer's rules and lists anything missing.",
      "Priya attaches the missing report and submits. The claim is recorded as AI-assisted, with Priya as the submitter.",
    ],
    modules: ["AI-assisted workflows", "Billing & insurance"],
  },
  {
    id: "cover",
    title: "Shift cover",
    summary: "A nurse on tonight's ICU shift calls in sick in the afternoon.",
    source: "nursing",
    agent: "rosters",
    approver: { name: "Sister Thomas", role: "Nursing superintendent" },
    outcome: "Roster updated",
    narration: [
      "The nursing office records the absence, and the supervisor agent picks up the gap in tonight's ICU roster.",
      "The supervisor asks the roster agent, which checks who holds ICU credentials and has had enough rest.",
      "The agent proposes three nurses who could cover, ranked, with the reason for each.",
      "Sister Thomas confirms the swap with the first nurse. The roster change is recorded as AI-assisted, approved by Sister Thomas.",
    ],
    modules: ["AI-assisted workflows", "Workforce & payroll"],
  },
  {
    id: "audit",
    title: "Pharmacy audit",
    summary: "Finance closes the month, and the pharmacy's stock and payments need checking.",
    source: "finance",
    agent: "audit",
    approver: { name: "Farah", role: "Internal auditor" },
    outcome: "Review closed",
    narration: [
      "Finance starts the month-end close, and the supervisor agent schedules a pharmacy review.",
      "The supervisor sends it to the audit review agent, which can read the stock and payment trail but not change it.",
      "The agent matches receipts, dispensing and supplier payments, and flags two corrections without a recorded reason.",
      "Farah checks both flags with the store and closes the review. The findings sit beside the agent's, and the trail is unchanged.",
    ],
    modules: ["AI-assisted workflows", "Finance & accounts"],
  },
];

export const aiNote =
  "AI assistance is planned. Each agent will be released only after privacy and clinical-safety validation, sees only what its task needs, and acts only when a named person approves.";

export function scenarioSteps(scenario: Scenario): { from: NodeId; to: NodeId; text: string }[] {
  const [event, routing, draft, approval] = scenario.narration;
  return [
    { from: scenario.source, to: "supervisor", text: event },
    { from: "supervisor", to: scenario.agent, text: routing },
    { from: scenario.agent, to: "person", text: draft },
    { from: "person", to: "record", text: approval },
  ];
}
