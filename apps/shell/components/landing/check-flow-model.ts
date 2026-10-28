export type FlowAgent = { label: string; detail: string };

// A route either has an agent that drafts or says why a person acts alone.
export type FlowRoute = {
  id: string;
  title: string;
  approver: { name: string; role: string };
  outcome: string;
} & ({ agent: FlowAgent; noAgent?: never } | { agent?: never; noAgent: string });

export type FlowStep = {
  id: string;
  phase: "register" | "check" | "draft" | "approve";
  route?: string;
  text: string;
};

export function flowStepTitle(
  step: FlowStep,
  routes: readonly FlowRoute[],
  registerTitle: string,
): string {
  if (step.phase === "register") return registerTitle;
  if (step.phase === "check") return "Automatic check";
  const route = routes.find((candidate) => candidate.id === step.route)!;
  if (step.phase === "draft") return route.agent!.label;
  return `${route.approver.name}, ${route.approver.role.toLowerCase()}`;
}
