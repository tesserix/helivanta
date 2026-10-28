import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import type { PharmacyAffiliationReview } from "@helivanta/api";
import { PharmacyAffiliationReviews } from "./pharmacy-affiliation-reviews";

const hospital = { id: "22222222-2222-4222-8222-222222222222", name: "TEST General" };

function review(over: Partial<PharmacyAffiliationReview>): PharmacyAffiliationReview {
  return {
    request_id: "0199e000-0000-7000-8000-0000000000aa",
    affiliation_id: "a1",
    pharmacy_id: "p1",
    pharmacy_name: "TEST Ward Pharmacy",
    hospital_id: hospital.id,
    relationship: "owner",
    status: "pending",
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    decisions: [],
    ...over,
  };
}

afterEach(() => vi.unstubAllGlobals());

function stub(rows: PharmacyAffiliationReview[]) {
  const fetcher = vi.fn(async (url: string, init: RequestInit = {}) => {
    if (init.method === "PUT") return new Response(JSON.stringify(rows[0]));
    expect(url).toContain(`/patients/pharmacy-affiliations?hospital_id=${hospital.id}`);
    return new Response(
      JSON.stringify({ data: rows, page: { has_more: false, next_cursor: null } }),
    );
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

const puts = (fetcher: ReturnType<typeof stub>) =>
  fetcher.mock.calls.filter(([, init]) => (init as RequestInit | undefined)?.method === "PUT") as [
    string,
    RequestInit,
  ][];

describe("PharmacyAffiliationReviews", () => {
  it("verifies a pending pharmacy with a reason", async () => {
    const fetcher = stub([review({})]);
    const { user } = renderWithProviders(<PharmacyAffiliationReviews hospital={hospital} />);
    expect(await screen.findByText("TEST Ward Pharmacy")).toBeInTheDocument();
    expect(screen.getByText(/Waiting for your decision/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Verify" }));
    expect(await screen.findByText("Give a reason")).toBeInTheDocument();
    expect(puts(fetcher)).toHaveLength(0);
    await user.type(screen.getByLabelText("Reason"), "Our hospital owns it");
    await user.click(screen.getByRole("button", { name: "Verify" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    const [url, init] = puts(fetcher)[0];
    expect(url).toMatch(
      /\/pharmacy-affiliations\/0199e000-0000-7000-8000-0000000000aa\/decision\/[0-9a-f-]{36}\?hospital_id=/,
    );
    expect(JSON.parse(init.body as string)).toEqual({
      outcome: "accepted",
      reason: "Our hospital owns it",
    });
  });

  it("declines without confirmation", async () => {
    const fetcher = stub([review({})]);
    const { user } = renderWithProviders(<PharmacyAffiliationReviews hospital={hospital} />);
    await user.type(await screen.findByLabelText("Reason"), "Not ours");
    await user.click(screen.getByRole("button", { name: "Decline" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(JSON.parse(puts(fetcher)[0][1].body as string).outcome).toBe("declined");
  });

  it("revokes a verified pharmacy only after confirming", async () => {
    const fetcher = stub([
      review({
        status: "accepted",
        decisions: [
          {
            id: "d1",
            outcome: "accepted",
            reason: "Owned",
            reviewer_subject: "user-a",
            created_at: new Date().toISOString(),
          },
        ],
      }),
    ]);
    const { user } = renderWithProviders(<PharmacyAffiliationReviews hospital={hospital} />);
    expect(await screen.findByText(/Verified · Owned/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Verify" })).toBeNull();
    await user.type(screen.getByLabelText("Reason"), "Sold");
    await user.click(screen.getByRole("button", { name: "Revoke" }));
    const dialog = await screen.findByRole("dialog");
    expect(puts(fetcher)).toHaveLength(0);
    await user.click(within(dialog).getByRole("button", { name: "Revoke" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(JSON.parse(puts(fetcher)[0][1].body as string)).toEqual({
      outcome: "revoked",
      reason: "Sold",
    });
  });

  it("offers no decision on a closed request", async () => {
    stub([review({ status: "withdrawn" })]);
    renderWithProviders(<PharmacyAffiliationReviews hospital={hospital} />);
    expect(await screen.findByText(/Withdrawn by the pharmacy/)).toBeInTheDocument();
    expect(screen.queryByRole("button")).toBeNull();
  });
});
