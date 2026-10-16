import { webcrypto } from "node:crypto";
import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { LicenceEvidence } from "./licence-evidence";
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "TEST-owner",
  partyId: "0199e000-0000-7000-8000-000000000001",
  licenseId: "0199e000-0000-7000-8000-000000000002",
};
const licence = { id: scope.licenseId, version: 2, number: "TEST-LIC" };
afterEach(() => vi.unstubAllGlobals());
it("shows source revisions and keeps auditors read-only despite broad global permissions", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async (url: string) =>
        new Response(
          JSON.stringify(
            url.endsWith("permissions")
              ? {
                  tenant_id: scope.tenantId,
                  subject: scope.subject,
                  data: ["supply.evidence.read", "supply.evidence.write"],
                }
              : {
                  data: [
                    {
                      id: "0199e000-0000-7000-8000-000000000003",
                      party_id: scope.partyId,
                      license_id: scope.licenseId,
                      license_version: 1,
                      filename: "TEST historical.png",
                      content_type: "image/png",
                      size: 1,
                      status: "rejected",
                      scan_attempt: 1,
                      uploaded_at: "2026-10-16T00:00:00Z",
                      scanned_at: "2026-10-16T00:00:00Z",
                      failure_code: "malware_detected",
                      uploader_subject: "TEST-uploader",
                      created_at: "2026-10-16T00:00:00Z",
                      updated_at: "2026-10-16T00:00:00Z",
                      expires_at: "2026-10-16T00:30:00Z",
                    },
                  ],
                  page: { has_more: false, next_cursor: null },
                },
          ),
        ),
    ),
  );
  renderWithProviders(
    <LicenceEvidence scope={scope} licence={licence} roles={["auditor"]} onClose={() => {}} />,
  );
  expect(await screen.findByText("TEST historical.png")).toBeInTheDocument();
  expect(screen.getByText(/Revision 1.*historical/)).toBeInTheDocument();
  expect(screen.queryByLabelText("Licence evidence image")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /Download/ })).not.toBeInTheDocument();
  expect(screen.getByText(/does not verify/)).toBeInTheDocument();
});
it("focuses the evidence panel and keeps upload failures beside the form", async () => {
  const fetcher = vi.fn(
    async (url: string) =>
      new Response(
        JSON.stringify(
          url.endsWith("permissions")
            ? {
                tenant_id: scope.tenantId,
                subject: scope.subject,
                data: ["supply.evidence.read", "supply.evidence.write"],
              }
            : { data: [], page: { has_more: false, next_cursor: null } },
        ),
      ),
  );
  vi.stubGlobal("fetch", fetcher);
  HTMLElement.prototype.scrollIntoView = vi.fn();
  renderWithProviders(
    <LicenceEvidence
      scope={scope}
      licence={licence}
      roles={["administrator"]}
      onClose={() => {}}
    />,
  );
  const input = await screen.findByLabelText("Licence evidence image");
  await waitFor(() =>
    expect(screen.getByRole("heading", { name: "Licence evidence" })).toHaveFocus(),
  );
  const user = userEvent.setup({ applyAccept: false });
  await user.upload(input, new File(["TEST"], "TEST.pdf", { type: "application/pdf" }));
  await user.click(screen.getByRole("button", { name: "Upload evidence" }));
  expect(
    await screen.findByText("Choose a PNG or JPEG image up to 10 MiB with a safe filename."),
  ).toBeInTheDocument();
  expect(fetcher.mock.calls.filter(([url]) => url.includes("/content"))).toHaveLength(0);
});

it("collapses rapid repeated form submission into one stable upload", async () => {
  vi.stubGlobal("crypto", webcrypto);
  const writes: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("permissions"))
        return new Response(
          JSON.stringify({
            tenant_id: scope.tenantId,
            subject: scope.subject,
            data: ["supply.evidence.read", "supply.evidence.write"],
          }),
        );
      if (init?.method === "PUT") {
        writes.push(url);
        return new Response(
          JSON.stringify({ error: "unavailable", message: "TEST upload unavailable" }),
          { status: 503 },
        );
      }
      return new Response(
        JSON.stringify({ data: [], page: { has_more: false, next_cursor: null } }),
      );
    }),
  );
  renderWithProviders(
    <LicenceEvidence
      scope={scope}
      licence={licence}
      roles={["administrator"]}
      onClose={() => {}}
    />,
  );
  const input = await screen.findByLabelText("Licence evidence image");
  await userEvent.setup().upload(input, new File(["TEST"], "TEST.png", { type: "image/png" }));
  const form = input.closest("form")!;
  act(() => {
    fireEvent.submit(form);
    fireEvent.submit(form);
  });
  expect(await screen.findByText("TEST upload unavailable")).toBeInTheDocument();
  expect(writes).toHaveLength(1);
  await userEvent.setup().click(screen.getByRole("button", { name: "Retry upload" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0]).toBe(writes[1]);
});
