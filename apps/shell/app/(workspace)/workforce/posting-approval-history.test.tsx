import { NavigationGuardProvider } from "@helivanta/ui";
import { renderWithProviders } from "@helivanta/api/testing";
import type { StaffPostingProposal } from "@helivanta/api";
import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { PostingApprovalHistory } from "./posting-approval-history";

const hospital = "22222222-2222-4222-8222-222222222222";
const staff = "33333333-3333-4333-8333-333333333333";
const id = (n: number) => `019a0000-0000-7000-8000-${String(n).padStart(12, "0")}`;
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-reviewer",
  hospitalId: hospital,
  staffId: staff,
  access: "scoped" as const,
};
const approved: StaffPostingProposal = {
  id: id(1),
  revision_id: id(2),
  command_id: id(3),
  hospital_id: hospital,
  staff_id: staff,
  version: 2,
  status: "approved",
  grants_access: false,
  draft_id: id(4),
  draft_revision_id: id(5),
  draft_version: 1,
  generation_id: id(6),
  staff_version: 2,
  employment_epoch: 2,
  department_id: id(7),
  department_revision_id: id(8),
  starts_at: "2026-11-01T00:00:00Z",
  ends_at: null,
  eligibility_until: "2027-01-01T00:00:00Z",
  created_at: "2026-11-01T00:00:00Z",
  recorded_at: "2026-11-01T00:00:01Z",
};
afterEach(() => vi.unstubAllGlobals());

it("asks again for retained approval history after an outage", async () => {
  const fetcher = vi.fn(async () =>
    Response.json({ error: "unavailable", message: "Synthetic outage" }, { status: 503 }),
  );
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PostingApprovalHistory
        scope={scope}
        proposal={approved}
        displayName="Synthetic Staff"
        onClose={vi.fn()}
        onContextChanged={vi.fn()}
      />
    </NavigationGuardProvider>,
  );
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "The retained approval history could not be verified.",
  );
  const reads = fetcher.mock.calls.length;
  await user.click(screen.getByRole("button", { name: "Try again" }));
  await waitFor(() => expect(fetcher.mock.calls.length).toBeGreaterThan(reads));
});
