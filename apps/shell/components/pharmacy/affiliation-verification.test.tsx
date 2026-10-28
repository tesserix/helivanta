import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import type { PharmacyHospitalAffiliation } from "@helivanta/api";
import { AffiliationVerification } from "./affiliation-verification";

const path = "/pharmacy/pharmacies/p1/hospital-affiliations/a1";
const request = "0199e000-0000-7000-8000-0000000000aa";

function link(over: Partial<PharmacyHospitalAffiliation>): PharmacyHospitalAffiliation {
  return {
    id: "a1",
    pharmacy_id: "p1",
    hospital_id: "h1",
    relationship: "owner",
    verification_status: "pending_validation",
    verification_request_id: null,
    verified_at: null,
    created_at: new Date().toISOString(),
    ...over,
  };
}

afterEach(() => vi.unstubAllGlobals());

function stub() {
  const fetcher = vi.fn(async () => new Response(JSON.stringify(link({}))));
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

describe("AffiliationVerification", () => {
  it("asks the hospital to verify an unverified link, and grants nothing until then", async () => {
    const fetcher = stub();
    const { user } = renderWithProviders(
      <AffiliationVerification link={link({})} path={path} invalidate={["k"]} canManage />,
    );
    expect(screen.getByText(/Not verified · No access granted/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Ask the hospital to verify" }));
    await waitFor(() => expect(fetcher).toHaveBeenCalled());
    const [url, init] = fetcher.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toMatch(/\/hospital-affiliations\/a1\/verification-requests\/[0-9a-f-]{36}$/);
    expect(init.method).toBe("PUT");
    expect(screen.queryByRole("button", { name: "Withdraw verification" })).toBeNull();
  });

  it("withdraws an open round with a reason, after confirming", async () => {
    const fetcher = stub();
    const { user } = renderWithProviders(
      <AffiliationVerification
        link={link({ verification_status: "verified", verification_request_id: request })}
        path={path}
        invalidate={["k"]}
        canManage
      />,
    );
    expect(screen.getByText(/Verified by the hospital/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Ask the hospital to verify" })).toBeNull();
    await user.click(screen.getByRole("button", { name: "Withdraw verification" }));
    await user.click(screen.getByRole("button", { name: "Withdraw" }));
    expect(await screen.findByText("Give a reason")).toBeInTheDocument();
    expect(fetcher).not.toHaveBeenCalled();
    await user.type(screen.getByLabelText("Reason for withdrawing"), "Outlet closed");
    await user.click(screen.getByRole("button", { name: "Withdraw" }));
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("Withdraw this hospital link?");
    expect(fetcher).not.toHaveBeenCalled();
    await user.click(within(dialog).getByRole("button", { name: "Withdraw" }));
    await waitFor(() => expect(fetcher).toHaveBeenCalled());
    const [url, init] = fetcher.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toContain(`/verification-requests/${request}/withdraw`);
    expect(JSON.parse(init.body as string)).toEqual({ reason: "Outlet closed" });
  });

  it.each([
    ["requested", /Waiting for the hospital to verify · No access granted/],
    ["declined", /Declined by the hospital · No access granted/],
    ["revoked", /Revoked by the hospital · No access granted/],
    ["withdrawn", /Withdrawn · No access granted/],
  ] as const)("labels %s honestly", (status, label) => {
    renderWithProviders(
      <AffiliationVerification
        link={link({ verification_status: status, verification_request_id: request })}
        path={path}
        invalidate={["k"]}
        canManage={false}
      />,
    );
    expect(screen.getByText(label)).toBeInTheDocument();
    expect(screen.queryByRole("button")).toBeNull();
  });
});
