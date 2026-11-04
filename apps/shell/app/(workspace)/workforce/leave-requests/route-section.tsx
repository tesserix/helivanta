import type { LeaveRequestRouting } from "@helivanta/api";

export function LeaveRouteSection({ routing }: { routing: LeaveRequestRouting }) {
  return (
    <section aria-label="Approval route">
      <h5 className="mb-2 text-sm font-semibold">Approval route</h5>
      {!routing.routed ? (
        <p className="text-sm text-muted-foreground">
          Filed under routing policy version {routing.version}. The hospital leave desk decides.
        </p>
      ) : routing.fallback ? (
        <p className="text-sm text-muted-foreground">
          No eligible reporting officer was found when this was filed under routing policy version{" "}
          {routing.version}. Only the hospital leave desk can decide it.
        </p>
      ) : (
        <>
          <ol className="space-y-1 text-sm">
            {routing.steps.map((step) => (
              <li key={step.level}>
                Level {step.level} · {step.display_name} · {step.employee_code} ·{" "}
                {step.open
                  ? `open since ${new Date(step.opens_at).toLocaleString()}`
                  : `opens ${new Date(step.opens_at).toLocaleString()}`}
              </li>
            ))}
          </ol>
          <p className="mt-2 text-xs text-muted-foreground">
            Pinned at filing under routing policy version {routing.version}. The hospital leave desk
            can decide at any time.
          </p>
        </>
      )}
    </section>
  );
}
