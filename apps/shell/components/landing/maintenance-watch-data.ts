import type { ModuleName } from "@/app/platform-catalog";
import { flowStepTitle, type FlowRoute, type FlowStep } from "./check-flow-model";

export type Finding = "current" | "due-soon" | "overdue" | "expiring" | "failed";

export type Area =
  "Electrical" | "Structural" | "Equipment" | "Fire safety" | "Lifts" | "Medical gas";

export type RegisterItem = {
  id: string;
  asset: string;
  area: Area;
  check: string;
  evidence: "task" | "certificate";
  // Days until the next check is due, or until a certificate lapses; negative once past.
  dueInDays: number;
  lastResult: "pass" | "fail";
};

export const DUE_SOON_DAYS = 14;
export const CERTIFICATE_WINDOW_DAYS = 60;

export const site = "Main hospital";

export const register: readonly RegisterItem[] = [
  {
    id: "main-panel",
    asset: "Main electrical panel",
    area: "Electrical",
    check: "Earthing and insulation test",
    evidence: "task",
    dueInDays: -12,
    lastResult: "pass",
  },
  {
    id: "block-b",
    asset: "Block B",
    area: "Structural",
    check: "Structural safety audit",
    evidence: "task",
    dueInDays: -45,
    lastResult: "pass",
  },
  {
    id: "icu-ventilator",
    asset: "ICU ventilator",
    area: "Equipment",
    check: "Flow sensor calibration",
    evidence: "task",
    dueInDays: 6,
    lastResult: "pass",
  },
  {
    id: "emergency-defibrillator",
    asset: "Emergency defibrillator",
    area: "Equipment",
    check: "Energy output test",
    evidence: "task",
    dueInDays: 30,
    lastResult: "fail",
  },
  {
    id: "fire-certificate",
    asset: "Fire safety certificate",
    area: "Fire safety",
    check: "Fire service renewal",
    evidence: "certificate",
    dueInDays: 38,
    lastResult: "pass",
  },
  {
    id: "bed-lift",
    asset: "Bed lift 2",
    area: "Lifts",
    check: "Lift operating licence",
    evidence: "certificate",
    dueInDays: 210,
    lastResult: "pass",
  },
  {
    id: "ward-extinguishers",
    asset: "Ward 3 extinguishers",
    area: "Fire safety",
    check: "Monthly inspection",
    evidence: "task",
    dueInDays: 18,
    lastResult: "pass",
  },
  {
    id: "oxygen-manifold",
    asset: "Oxygen manifold",
    area: "Medical gas",
    check: "Pressure and alarm test",
    evidence: "task",
    dueInDays: 25,
    lastResult: "pass",
  },
];

// A failed result outranks every date: a failed device is unsafe however recent its test.
export function findingOf(item: RegisterItem): Finding {
  if (item.lastResult === "fail") return "failed";
  if (item.dueInDays < 0) return "overdue";
  if (item.evidence === "certificate") {
    return item.dueInDays <= CERTIFICATE_WINDOW_DAYS ? "expiring" : "current";
  }
  return item.dueInDays <= DUE_SOON_DAYS ? "due-soon" : "current";
}

export const findingLabel: Record<Finding, string> = {
  current: "Current",
  "due-soon": "Due soon",
  overdue: "Overdue",
  expiring: "Certificate expiring",
  failed: "Failed check",
};

export function tally(): Record<Finding, number> {
  const counts: Record<Finding, number> = {
    current: 0,
    "due-soon": 0,
    overdue: 0,
    expiring: 0,
    failed: 0,
  };
  for (const item of register) counts[findingOf(item)] += 1;
  return counts;
}

export type RouteId = "work" | "renewal" | "failed";

export type Route = FlowRoute & { id: RouteId; findings: readonly Finding[] };

export const routes: readonly Route[] = [
  {
    id: "work",
    title: "Overdue or due soon",
    findings: ["overdue", "due-soon"],
    agent: {
      label: "Maintenance planning agent",
      detail: "Drafts work orders with a priority and due time",
    },
    approver: { name: "Rahul", role: "Engineering manager" },
    outcome: "Work orders issued",
  },
  {
    id: "renewal",
    title: "Certificate expiring",
    findings: ["expiring"],
    agent: { label: "Compliance agent", detail: "Drafts the renewal pack from the evidence" },
    approver: { name: "Farah", role: "Compliance manager" },
    outcome: "Renewal pack approved",
  },
  {
    id: "failed",
    title: "Failed safety check",
    findings: ["failed"],
    noAgent: "A failed safety check is an engineer's call",
    approver: { name: "Anil", role: "Biomedical engineer" },
    outcome: "Out of service until retested",
  },
];

export function routeItems(route: Route): RegisterItem[] {
  return register.filter((item) => route.findings.includes(findingOf(item)));
}

export type Step = FlowStep & { route?: RouteId };

export const steps: readonly Step[] = [
  {
    id: "register",
    phase: "register",
    text: "Every asset, inspection and certificate on the site is on one register: what is checked, how often, by whom and the last result.",
  },
  {
    id: "check",
    phase: "check",
    text: `Each night and after every inspection, the check compares each due date and certificate with today. It flags work due within ${DUE_SOON_DAYS} days and certificates within ${CERTIFICATE_WINDOW_DAYS}, and sorts out what needs a decision.`,
  },
  {
    id: "work-draft",
    phase: "draft",
    route: "work",
    text: "The main electrical panel's earthing test is 12 days overdue, the Block B structural audit is 45 days overdue and the ICU ventilator's calibration is due in 6 days. The agent drafts a work order for each, with a priority and a due time.",
  },
  {
    id: "work-approve",
    phase: "approve",
    route: "work",
    text: "Rahul raises the structural audit to urgent and approves. No work order is issued without approval, and each one is marked AI-assisted.",
  },
  {
    id: "renewal",
    phase: "approve",
    route: "renewal",
    text: "The fire safety certificate expires in 38 days. The agent drafts the renewal pack from the latest drill, extinguisher and hydrant records and lists what is missing. Farah approves it before anyone books the inspection.",
  },
  {
    id: "failed",
    phase: "approve",
    route: "failed",
    text: "The emergency defibrillator failed its energy output test. No agent is involved: Anil takes it out of service, and finishing the repair does not bring it back. It returns only when Anil releases it after a passing retest.",
  },
];

export function stepTitle(step: Step): string {
  return flowStepTitle(step, routes, "Maintenance register");
}

export const maintenanceModules: readonly ModuleName[] = [
  "Assets & facilities",
  "Safety & quality",
  "AI-assisted workflows",
];

export const maintenanceNote =
  "Nothing on this board is available yet. The maintenance register, inspections, certificates and the automatic check are planned in FacilityConnect, and the agents are a future capability that will act only when a named person approves. Which inspections and certificates apply, and how often, depends on the country, state and type of facility. Helivanta keeps the evidence; it does not certify compliance.";
