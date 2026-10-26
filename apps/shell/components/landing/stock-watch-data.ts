import type { ModuleName } from "@/app/platform-catalog";
import { flowStepTitle, type FlowRoute, type FlowStep } from "./check-flow-model";

export type Finding = "in-stock" | "low" | "out" | "expiring" | "quarantine";

export type ShelfItem = {
  id: string;
  medicine: string;
  unit: string;
  dispensable: number;
  quarantined: number;
  reorderAt: number;
  expiresInDays?: number;
};

export const EXPIRY_WINDOW_DAYS = 30;

export const outlet = "Main pharmacy";

export const shelf: readonly ShelfItem[] = [
  {
    id: "paracetamol",
    medicine: "Paracetamol 500 mg",
    unit: "tablets",
    dispensable: 4200,
    quarantined: 0,
    reorderAt: 1500,
    expiresInDays: 420,
  },
  {
    id: "amoxicillin",
    medicine: "Amoxicillin 500 mg",
    unit: "capsules",
    dispensable: 640,
    quarantined: 0,
    reorderAt: 900,
    expiresInDays: 300,
  },
  {
    id: "insulin",
    medicine: "Insulin glargine",
    unit: "pens",
    dispensable: 0,
    quarantined: 0,
    reorderAt: 40,
  },
  {
    id: "metformin",
    medicine: "Metformin 500 mg",
    unit: "tablets",
    dispensable: 2800,
    quarantined: 0,
    reorderAt: 1200,
    expiresInDays: 24,
  },
  {
    id: "ceftriaxone",
    medicine: "Ceftriaxone 1 g",
    unit: "vials",
    dispensable: 60,
    quarantined: 180,
    reorderAt: 40,
    expiresInDays: 510,
  },
  {
    id: "ondansetron",
    medicine: "Ondansetron 4 mg",
    unit: "tablets",
    dispensable: 900,
    quarantined: 0,
    reorderAt: 300,
    expiresInDays: 250,
  },
];

// One precedence, so a medicine that is both short and expiring takes one route.
export function findingOf(item: ShelfItem): Finding {
  if (item.dispensable === 0) return "out";
  if (item.quarantined > 0) return "quarantine";
  if (item.dispensable < item.reorderAt) return "low";
  if (item.expiresInDays !== undefined && item.expiresInDays <= EXPIRY_WINDOW_DAYS) {
    return "expiring";
  }
  return "in-stock";
}

export const findingLabel: Record<Finding, string> = {
  "in-stock": "In stock",
  low: "Running low",
  out: "Out of stock",
  expiring: `Expires within ${EXPIRY_WINDOW_DAYS} days`,
  quarantine: "In quarantine",
};

export function tally(): Record<Finding, number> {
  const counts: Record<Finding, number> = {
    "in-stock": 0,
    low: 0,
    out: 0,
    expiring: 0,
    quarantine: 0,
  };
  for (const item of shelf) counts[findingOf(item)] += 1;
  return counts;
}

export type RouteId = "order" | "expiry" | "quarantine";

export type Route = FlowRoute & { id: RouteId; findings: readonly Finding[] };

export const routes: readonly Route[] = [
  {
    id: "order",
    title: "To order",
    findings: ["low", "out"],
    agent: { label: "Stock forecasting agent", detail: "Drafts one purchase request" },
    approver: { name: "Imran", role: "Pharmacy manager" },
    outcome: "Purchase request raised",
  },
  {
    id: "expiry",
    title: "Expiring soon",
    findings: ["expiring"],
    agent: { label: "Stock forecasting agent", detail: "Suggests use first and a transfer" },
    approver: { name: "Meera", role: "Store in-charge" },
    outcome: "Transfer approved",
  },
  {
    id: "quarantine",
    title: "Held in quarantine",
    findings: ["quarantine"],
    noAgent: "A held batch is a pharmacist's call",
    approver: { name: "Kavya", role: "Pharmacist" },
    outcome: "Batch released",
  },
];

export function routeItems(route: Route): ShelfItem[] {
  return shelf.filter((item) => route.findings.includes(findingOf(item)));
}

export type Step = FlowStep & { route?: RouteId };

export const steps: readonly Step[] = [
  {
    id: "register",
    phase: "register",
    text: "Every batch is on the register: what can be dispensed, what waits in quarantine and when it expires.",
  },
  {
    id: "check",
    phase: "check",
    text: "Each night and after every dispense, the check compares every medicine with its reorder level and expiry date, and sorts out what needs a decision.",
  },
  {
    id: "order-draft",
    phase: "draft",
    route: "order",
    text: "Amoxicillin is below its reorder level and insulin pens are out. The agent drafts one purchase request and shows the usage, open orders and supplier lead time behind each quantity.",
  },
  {
    id: "order-approve",
    phase: "approve",
    route: "order",
    text: "Imran lowers one quantity and approves. Nothing is ordered without approval, and the request reaches purchase marked AI-assisted.",
  },
  {
    id: "expiry",
    phase: "approve",
    route: "expiry",
    text: "A metformin batch expires in 24 days. The agent suggests dispensing it first and moving part of it to the ward that uses the most, and Meera approves the transfer.",
  },
  {
    id: "quarantine",
    phase: "approve",
    route: "quarantine",
    text: "A ceftriaxone delivery waits in quarantine. No agent is involved: Kavya checks the batch and releases it for dispensing.",
  },
];

export function stepTitle(step: Step): string {
  return flowStepTitle(step, routes, "Stock register");
}

export const stockModules: readonly ModuleName[] = [
  "Pharmacy management",
  "Inventory & procurement",
  "AI-assisted workflows",
];

export const stockNote =
  "Available today in the pharmacy: every batch with what can be dispensed, what is held in quarantine and how soon it expires. Reorder levels, the automatic check, the agent's drafts and release from quarantine are planned, and every agent will act only when a named person approves.";
