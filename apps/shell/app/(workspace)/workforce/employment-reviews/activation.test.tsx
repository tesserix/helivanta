import { afterEach, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { NavigationGuardProvider } from "@helivanta/ui";
import { renderWithProviders } from "@helivanta/api/testing";
import { EmploymentActivation } from "./activation";

const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  staffId: "33333333-3333-4333-8333-333333333333",
  subject: "synthetic-activator",
  hrIntake: true as const,
};
const review = {
  id: "019a0000-0000-7000-8000-000000000001",
  revision_id: "019a0000-0000-7000-8000-000000000002",
  version: 2,
};
const source = {
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  review_id: review.id,
  review_revision_id: review.revision_id,
  review_version: 2,
  staff_version: 1,
  employment_epoch: 1,
  current_state: "eligible",
  can_activate: true,
  starts_at: "2026-11-01T00:00:00Z",
  ends_at: null,
};
afterEach(() => vi.unstubAllGlobals());
function mount() {
  return renderWithProviders(
    <NavigationGuardProvider>
      <EmploymentActivation scope={scope} review={review} onBusy={vi.fn()} onAccessLost={vi.fn()} />
    </NavigationGuardProvider>,
  );
}
it("requires a reason and confirmation before sending activation", async () => {
  const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(source), { status: 200 }));
  vi.stubGlobal("fetch", fetch);
  const { user } = mount();
  await user.click(screen.getByRole("button", { name: "Check activation eligibility" }));
  await screen.findByRole("button", { name: "Activate employment" });
  await user.click(screen.getByRole("button", { name: "Activate employment" }));
  await waitFor(() =>
    expect(screen.getByLabelText(/Activation reason/)).toHaveAttribute("aria-invalid", "true"),
  );
  expect(fetch).toHaveBeenCalledTimes(1);
});
it("keeps the same frozen command after an uncertain server failure", async () => {
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(new Response(JSON.stringify(source), { status: 200 }))
    .mockResolvedValue(
      new Response(JSON.stringify({ error: "internal", message: "Synthetic retryable failure" }), {
        status: 500,
      }),
    );
  vi.stubGlobal("fetch", fetch);
  const { user } = mount();
  await user.click(screen.getByRole("button", { name: "Check activation eligibility" }));
  await user.type(
    await screen.findByLabelText(/Activation reason/),
    "Synthetic reviewed appointment",
  );
  await user.click(screen.getByRole("checkbox"));
  await user.click(screen.getByRole("button", { name: "Activate employment" }));
  await screen.findByRole("button", { name: "Retry same activation" });
  expect(screen.getByLabelText(/Activation reason/)).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Retry same activation" }));
  await waitFor(() => expect(fetch).toHaveBeenCalledTimes(3));
  expect(fetch.mock.calls[1]![0]).toBe(fetch.mock.calls[2]![0]);
  expect(fetch.mock.calls[1]![1].body).toBe(fetch.mock.calls[2]![1].body);
});
it("does not offer activation for future appointments", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        new Response(
          JSON.stringify({ ...source, current_state: "not_started", can_activate: false }),
          { status: 200 },
        ),
      ),
  );
  const { user } = mount();
  await user.click(screen.getByRole("button", { name: "Check activation eligibility" }));
  await screen.findByText("The appointment has not started.");
  expect(screen.queryByRole("button", { name: "Activate employment" })).not.toBeInTheDocument();
});
it("shows a verified activation receipt without claiming login or role access", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, request?: RequestInit) => {
      if (request?.method !== "PUT") return Response.json(source);
      const input = JSON.parse(String(request.body));
      return Response.json({
        generation: {
          id: new URL(url, "http://local.test").pathname.split("/").at(-1),
          revision_id: input.command_id,
          hospital_id: scope.hospitalId,
          staff_id: scope.staffId,
          version: 1,
          review_id: review.id,
          review_revision_id: review.revision_id,
          review_version: 2,
          review_staff_version: 1,
          review_employment_epoch: 1,
          staff_version: 2,
          employment_epoch: 2,
          identity_hash: "a".repeat(64),
          starts_at: source.starts_at,
          ends_at: null,
          access_intent: "digital",
          declared_subject: "synthetic-recipient",
          activated_by_subject: scope.subject,
          reason: input.reason,
          activated_at: "2026-11-01T01:00:00Z",
        },
        current_state: "effective",
        grants_access: false,
      });
    }),
  );
  const { user } = mount();
  await user.click(screen.getByRole("button", { name: "Check activation eligibility" }));
  await user.type(
    await screen.findByLabelText(/Activation reason/),
    "Synthetic independently approved appointment",
  );
  await user.click(screen.getByRole("checkbox"));
  await user.click(screen.getByRole("button", { name: "Activate employment" }));
  await screen.findByText("Employment activation recorded.");
  expect(
    screen.getByText("Login and role approvals are still separate steps."),
  ).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Activate employment" })).not.toBeInTheDocument();
});
