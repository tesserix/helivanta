import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { Prescriptions } from "./prescriptions";

const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const rxId = "0199e000-0000-7000-8000-000000000040";
const page = (data: unknown[]) => ({
  data,
  page: { has_more: false, next_cursor: null, total: data.length, offset: 0 },
});
const prescription = {
  id: rxId,
  outlet_id: scope.outletId,
  prescriber_name: "TEST Dr Asha Rao",
  prescriber_registration: "MMC/2011/04567",
  prescriber_council: "Maharashtra Medical Council",
  prescribed_on: "2026-10-30",
  patient_name: "TEST Ravi Kumar",
  patient_address: null,
  recorded_by: "user-pharmacist",
  created_at: "2026-11-01T06:30:00Z",
};
const image = (id: string, status: string, failure_code: string | null = null) => ({
  id,
  outlet_id: scope.outletId,
  prescription_id: rxId,
  filename: `TEST-${status}.png`,
  content_type: "image/png",
  size: 68,
  status,
  scan_attempt: 1,
  uploaded_at: "2026-11-01T06:31:00Z",
  scanned_at: status === "available" ? "2026-11-01T06:32:00Z" : null,
  failure_code,
  uploader_subject: "user-pharmacist",
  created_at: "2026-11-01T06:30:30Z",
  updated_at: "2026-11-01T06:32:00Z",
  expires_at: "2026-11-01T07:00:30Z",
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function fixture(options: { record?: (init: RequestInit) => Response; images?: unknown[] } = {}) {
  const fetcher = vi.fn(async (url: string, init: RequestInit = {}) => {
    if (/\/prescriptions\/[0-9a-f-]{36}$/.test(url) && init.method === "PUT")
      return (
        options.record?.(init) ??
        new Response(JSON.stringify({ ...prescription, ...JSON.parse(String(init.body)) }), {
          status: 201,
        })
      );
    if (url.includes("/prescriptions?")) return new Response(JSON.stringify(page([prescription])));
    if (url.includes(`/prescriptions/${rxId}/images/`) && url.endsWith("/retry/2"))
      return new Response(
        JSON.stringify({
          ...image("0199e000-0000-7000-8000-000000000052", "scan_pending"),
          scan_attempt: 2,
        }),
        { status: 202 },
      );
    if (url.includes(`/prescriptions/${rxId}/images`))
      return new Response(JSON.stringify(page(options.images ?? [])));
    return new Response(JSON.stringify({ message: `unexpected ${url}` }), { status: 500 });
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

async function fillPrescription(user: ReturnType<typeof renderWithProviders>["user"]) {
  await user.type(screen.getByLabelText("Prescriber"), "TEST Dr Asha Rao");
  await user.type(screen.getByLabelText("Registration number"), "MMC/2011/04567");
  await user.type(screen.getByLabelText("Council"), "Maharashtra Medical Council");
  await user.type(screen.getByLabelText("Prescription date"), "2026-10-30");
  await user.type(screen.getByLabelText("Patient"), "TEST Ravi Kumar");
}

describe("Prescriptions", () => {
  it("records a prescription without an empty address and opens its pages", async () => {
    const fetcher = fixture();
    const { user } = renderWithProviders(<Prescriptions scope={scope} canRecord />);
    expect(await screen.findByText("TEST Ravi Kumar")).toBeVisible();
    await fillPrescription(user);
    await user.click(screen.getByRole("button", { name: "Record prescription" }));
    expect(await screen.findByText("Prescription recorded")).toBeVisible();
    const put = fetcher.mock.calls.find(([, init]) => init?.method === "PUT");
    expect(JSON.parse(String(put?.[1]?.body))).toEqual({
      prescriber_name: "TEST Dr Asha Rao",
      prescriber_registration: "MMC/2011/04567",
      prescriber_council: "Maharashtra Medical Council",
      prescribed_on: "2026-10-30",
      patient_name: "TEST Ravi Kumar",
    });
    expect(await screen.findByRole("heading", { name: "Pages for TEST Ravi Kumar" })).toBeVisible();
    expect(screen.getByText("No pages attached yet.")).toBeVisible();
    expect(screen.getByLabelText("Prescriber")).toHaveValue("");
  });

  it("shows inline errors and sends nothing for an incomplete prescription", async () => {
    const fetcher = fixture();
    const { user } = renderWithProviders(<Prescriptions scope={scope} canRecord />);
    await screen.findByText("TEST Ravi Kumar");
    await user.type(screen.getByLabelText("Registration number"), "MMC;2011");
    await user.click(screen.getByRole("button", { name: "Record prescription" }));
    expect(await screen.findByText("Enter the prescriber's name")).toBeVisible();
    expect(screen.getByText(/Use the registration number as printed/)).toBeVisible();
    expect(fetcher.mock.calls.some(([, init]) => init?.method === "PUT")).toBe(false);
  });

  it("shows the server's reason when the id was already used with different details", async () => {
    fixture({
      record: () =>
        new Response(
          JSON.stringify({
            error: "conflict",
            message:
              "this prescription was already recorded with different details; record a new prescription",
          }),
          { status: 409 },
        ),
    });
    const { user } = renderWithProviders(<Prescriptions scope={scope} canRecord />);
    await screen.findByText("TEST Ravi Kumar");
    await fillPrescription(user);
    await user.click(screen.getByRole("button", { name: "Record prescription" }));
    expect(await screen.findByText(/already recorded with different details/)).toBeVisible();
  });

  it("offers a page only once it is released, and retries a failed scan on the next attempt", async () => {
    const fetcher = fixture({
      images: [
        image("0199e000-0000-7000-8000-000000000050", "available"),
        image("0199e000-0000-7000-8000-000000000051", "rejected", "malware_detected"),
        image("0199e000-0000-7000-8000-000000000052", "scan_error", "scanner_unavailable"),
        image("0199e000-0000-7000-8000-000000000053", "scan_pending"),
      ],
    });
    const { user } = renderWithProviders(<Prescriptions scope={scope} canRecord />);
    await user.click(await screen.findByRole("button", { name: "Open pages for TEST Ravi Kumar" }));
    expect(await screen.findByRole("button", { name: "Open TEST-available.png" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "Open TEST-rejected.png" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Open TEST-scan_pending.png" })).toBeNull();
    expect(screen.getByText(/Rejected — the scan found a threat/)).toBeVisible();
    expect(screen.getByText(/^Scanning/)).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() =>
      expect(
        fetcher.mock.calls.some(
          ([url, init]) => String(url).endsWith("/retry/2") && init?.method === "POST",
        ),
      ).toBe(true),
    );
  });

  it("lets a reader open pages but not record or retry", async () => {
    fixture({ images: [image("0199e000-0000-7000-8000-000000000052", "scan_error")] });
    const { user } = renderWithProviders(<Prescriptions scope={scope} canRecord={false} />);
    expect(screen.queryByRole("button", { name: "Record prescription" })).toBeNull();
    await user.click(await screen.findByRole("button", { name: "Open pages for TEST Ravi Kumar" }));
    expect(await screen.findByText(/Scan failed/)).toBeVisible();
    expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
    expect(screen.queryByLabelText("Scanned page")).toBeNull();
  });
});
