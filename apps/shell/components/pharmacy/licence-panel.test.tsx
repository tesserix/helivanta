import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import type { PharmacyLicence, PharmacyLicenceEvidence } from "@helivanta/api";
import { LicencePanel } from "./licence-panel";

const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const licence: PharmacyLicence = {
  id: "0199e000-0000-7000-8000-000000000020",
  outlet_id: scope.outletId,
  licence_kind: "retail",
  form_label: "TEST Form 20",
  licence_number: "TEST-DL-001",
  issuing_authority: "TEST Drugs Controller",
  valid_from: "2026-01-01",
  valid_until: "2026-12-01",
  pharmacist_name: "TEST Pharmacist",
  pharmacist_registration: "TEST-PCI-001",
  entered_by: "user-author",
  created_at: "2026-11-01T06:30:00Z",
  decision: null,
  decision_reason: null,
  reviewed_by: null,
  reviewed_at: null,
  reviewed_evidence_file_id: null,
};
const evidence: PharmacyLicenceEvidence = {
  id: "0199e000-0000-7000-8000-000000000030",
  outlet_id: scope.outletId,
  licence_id: licence.id,
  filename: "TEST-licence.png",
  content_type: "image/png",
  size: 68,
  status: "scan_pending",
  scan_attempt: 1,
  uploaded_at: "2026-11-01T06:30:00Z",
  scanned_at: null,
  failure_code: null,
  uploader_subject: "user-author",
  expires_at: "2026-11-01T07:00:00Z",
  created_at: "2026-11-01T06:30:00Z",
  updated_at: "2026-11-01T06:30:00Z",
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.clear();
});

function fixture(
  options: { author?: string; image?: PharmacyLicenceEvidence; denyImage?: boolean } = {},
) {
  let saved = { ...licence, entered_by: options.author ?? licence.entered_by };
  const state = { image: options.image ?? { ...evidence } };
  const fetcher = vi.fn(async (url: string, init: RequestInit = {}) => {
    if (url.endsWith("/iam/me/permissions"))
      return new Response(
        JSON.stringify({
          subject: "user-reviewer",
          tenant_id: scope.tenantId,
          data: [
            "pharmacy.licence.manage",
            "pharmacy.licence.review",
            "pharmacy.licence.evidence.read",
          ],
        }),
      );
    if (url.endsWith("/licences/renewals"))
      return new Response(
        JSON.stringify(
          page([
            {
              licence_id: licence.id,
              date_status: "renewal_due_30",
              days_remaining: 30,
              local_date: "2026-11-01",
              time_zone: "Asia/Kolkata",
              observed_at: "2026-11-01T06:30:00Z",
            },
          ]),
        ),
      );
    if (url.endsWith("/licences")) return new Response(JSON.stringify(page([saved])));
    if (url.includes("/evidence?limit=1"))
      return options.denyImage
        ? new Response(JSON.stringify({ error: "forbidden", message: "TEST denied" }), {
            status: 403,
          })
        : new Response(JSON.stringify(page([state.image])));
    if (url.endsWith("/review")) {
      if (state.image.status !== "available")
        return new Response(
          JSON.stringify({
            error: "licence_evidence_required",
            message: "Wait for the clean scan",
          }),
          { status: 409 },
        );
      const input = JSON.parse(String(init.body));
      saved = {
        ...saved,
        decision: input.decision,
        decision_reason: input.reason,
        reviewed_by: "user-reviewer",
        reviewed_at: "2026-11-01T06:35:00Z",
        reviewed_evidence_file_id: state.image.id,
      };
      return new Response(JSON.stringify(saved));
    }
    throw new Error(`unexpected ${init.method ?? "GET"} ${url}`);
  });
  vi.stubGlobal("fetch", fetcher);
  return { fetcher, state };
}

describe("Pharmacy licence setup", () => {
  it("shows dated renewal advice and holds review until a clean image is available", async () => {
    const { fetcher, state } = fixture();
    const { user } = renderWithProviders(
      <LicencePanel scope={scope} subject="user-reviewer" today="2026-11-01" />,
    );
    await user.click(await screen.findByText(/TEST-DL-001 · Awaiting independent review/));
    expect(await screen.findByText(/Renewal due within 30 days · 30 days remaining/)).toBeVisible();
    const review = await screen.findByRole("button", { name: "Record independent licence review" });
    expect(review).toBeDisabled();
    expect(fetcher.mock.calls.filter(([url]) => url.endsWith("/review"))).toHaveLength(0);
    state.image = { ...evidence, status: "available", scanned_at: "2026-11-01T06:31:00Z" };
    await waitFor(() => expect(review).toBeEnabled(), { timeout: 6000 });
    await user.type(
      screen.getByLabelText("Licence review reason"),
      "TEST inspected the issued original and retained image",
    );
    await user.click(review);
    expect(await screen.findByText(/TEST-DL-001 · Approved/)).toBeVisible();
    const request = fetcher.mock.calls.find(([url]) => url.endsWith("/review"));
    expect(request?.[1]?.method).toBe("POST");
    expect(JSON.parse(String(request?.[1]?.body))).toEqual({
      decision: "approved",
      reason: "TEST inspected the issued original and retained image",
    });
  });

  it("does not offer self-review even for a clean image", async () => {
    fixture({
      author: "user-reviewer",
      image: { ...evidence, status: "available", scanned_at: "2026-11-01T06:31:00Z" },
    });
    const { user } = renderWithProviders(
      <LicencePanel scope={scope} subject="user-reviewer" today="2026-11-01" />,
    );
    await user.click(await screen.findByText(/TEST-DL-001 · Awaiting independent review/));
    expect(
      await screen.findByText("Another authorized owner must review this declaration."),
    ).toBeVisible();
    expect(screen.queryByRole("button", { name: "Record independent licence review" })).toBeNull();
  });

  it("keeps review refused when evidence access is unavailable", async () => {
    fixture({ denyImage: true });
    const { user } = renderWithProviders(
      <LicencePanel scope={scope} subject="user-reviewer" today="2026-11-01" />,
    );
    await user.click(await screen.findByText(/TEST-DL-001 · Awaiting independent review/));
    expect(
      await screen.findByText("This wasn't found, or you don't have access to it."),
    ).toBeVisible();
    expect(screen.queryByRole("button", { name: /^Try again$/ })).toBeNull();
    expect(
      screen.getByRole("button", { name: "Record independent licence review" }),
    ).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Download licence image" })).toBeNull();
  });
});
