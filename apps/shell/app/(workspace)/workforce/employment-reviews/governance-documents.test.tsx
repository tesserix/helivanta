import { webcrypto } from "node:crypto";
import { screen, within } from "@testing-library/react";
import { NavigationGuardProvider } from "@helivanta/ui";
import { renderWithProviders } from "@helivanta/api/testing";
import { GovernanceEmploymentDocuments } from "./governance-documents";
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-owner",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  staffId: "33333333-3333-4333-8333-333333333333",
  staffVersion: 3,
  employmentEpoch: 2,
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
it("retains the same upload intent and bytes after an uncertain registration", async () => {
  vi.stubGlobal("crypto", webcrypto);
  const registrations: { url: string; body: unknown }[] = [];
  const bodies: BodyInit[] = [];
  let row: Record<string, unknown> | undefined;
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method === "PUT" && url.includes("/content?")) {
      bodies.push(init.body!);
      row = {
        ...row,
        status: "scan_pending",
        uploaded_at: "2026-11-01T00:01:00Z",
        updated_at: "2026-11-01T00:01:00Z",
      };
      return Response.json(row);
    }
    if (init?.method === "PUT") {
      registrations.push({ url, body: JSON.parse(String(init.body)) });
      if (registrations.length === 1)
        return Response.json({ error: "unavailable", message: "Uncertain save" }, { status: 503 });
      row = {
        ...JSON.parse(String(init.body)),
        id: url.split("?")[0].split("/").at(-1),
        hospital_id: scope.hospitalId,
        staff_id: scope.staffId,
        record_version: 3,
        status: "upload_pending",
        scan_attempt: 1,
        uploaded_at: null,
        scanned_at: null,
        failure_code: null,
        created_at: "2026-11-01T00:00:00Z",
        updated_at: "2026-11-01T00:00:00Z",
        expires_at: "2026-11-01T00:30:00Z",
      };
      return Response.json(row);
    }
    return Response.json(page(row ? [row] : []));
  });
  vi.stubGlobal("fetch", fetcher);
  const busy = vi.fn();
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <GovernanceEmploymentDocuments
        scope={scope}
        onBusy={busy}
        onClose={() => {}}
        onAccessLost={() => {}}
      />
    </NavigationGuardProvider>,
  );
  await screen.findByText("No employment files prepared.");
  const selected = new File(["x"], "synthetic-appointment.png", { type: "image/png" });
  await user.upload(screen.getByLabelText("Employment evidence image"), selected);
  await user.click(screen.getByRole("button", { name: "Upload employment file" }));
  await screen.findByText(
    "Could not confirm the file action. Retry the same action to recover its result.",
  );
  expect(screen.getByLabelText("Employment evidence image")).toBeDisabled();
  expect(screen.getByRole("button", { name: "Close employment files" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Retry same file action" }));
  await screen.findByText("File received. Wait for scanning before attaching it to terms.");
  expect(registrations).toHaveLength(2);
  expect(registrations[1]).toEqual(registrations[0]);
  expect(bodies).toEqual([selected]);
  const entry = screen.getByRole("listitem");
  expect(within(entry).getByText("scan_pending")).toBeInTheDocument();
  expect(within(entry).queryByRole("button", { name: "Download" })).not.toBeInTheDocument();
  for (const [url, init] of fetcher.mock.calls) {
    expect(url).toContain("employment-preparation-documents");
    expect(url).toContain("staff_version=3&employment_epoch=2");
    expect(new Headers(init?.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  }
  expect(busy).toHaveBeenCalledWith(true);
});
it("closes access without legacy fallback when the original source is denied", async () => {
  const lost = vi.fn();
  const fetcher = vi.fn(async () =>
    Response.json({ error: "forbidden", message: "Denied" }, { status: 403 }),
  );
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(
    <NavigationGuardProvider>
      <GovernanceEmploymentDocuments
        scope={scope}
        onBusy={() => {}}
        onClose={() => {}}
        onAccessLost={lost}
      />
    </NavigationGuardProvider>,
  );
  await screen.findByRole("alert");
  expect(lost).toHaveBeenCalledTimes(1);
  expect(fetcher).toHaveBeenCalledTimes(1);
  expect(screen.queryByLabelText("Employment evidence image")).not.toBeInTheDocument();
});

it("recovers an incomplete upload with its original file identifier", async () => {
  vi.stubGlobal("crypto", webcrypto);
  const original = {
    id: "019a0000-0000-7000-8000-000000000099",
    hospital_id: scope.hospitalId,
    staff_id: scope.staffId,
    record_id: scope.staffId,
    record_kind: "enrollment",
    record_version: 3,
    category: "employment_evidence",
    filename: "synthetic-original.png",
    content_type: "image/png",
    size: 1,
    sha256: "2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881",
    status: "upload_pending",
    scan_attempt: 1,
    uploaded_at: null,
    scanned_at: null,
    failure_code: null,
    created_at: "2026-11-01T00:00:00Z",
    updated_at: "2026-11-01T00:00:00Z",
    expires_at: "2026-11-01T00:30:00Z",
  };
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method !== "PUT") return Response.json(page([original]));
    return Response.json(
      url.includes("/content?")
        ? {
            ...original,
            status: "scan_pending",
            uploaded_at: "2026-11-01T00:01:00Z",
            updated_at: "2026-11-01T00:01:00Z",
          }
        : original,
    );
  });
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <GovernanceEmploymentDocuments
        scope={scope}
        onBusy={() => {}}
        onClose={() => {}}
        onAccessLost={() => {}}
      />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Resume upload" }));
  await user.upload(
    screen.getByLabelText("Employment evidence image"),
    new File(["x"], original.filename, { type: original.content_type }),
  );
  await user.click(screen.getByRole("button", { name: "Upload employment file" }));
  await screen.findByText("File received. Wait for scanning before attaching it to terms.");
  const writes = fetcher.mock.calls.filter(([, init]) => init?.method === "PUT");
  expect(writes).toHaveLength(2);
  for (const [url] of writes) expect(url).toContain(`/${original.id}`);
});

it("retains a scan retry attempt on uncertainty and requires confirmation to revoke", async () => {
  let row = {
    id: "019a0000-0000-7000-8000-000000000088",
    hospital_id: scope.hospitalId,
    staff_id: scope.staffId,
    record_id: scope.staffId,
    record_kind: "enrollment",
    record_version: 3,
    category: "employment_evidence",
    filename: "synthetic-scan.png",
    content_type: "image/png",
    size: 1,
    sha256: "a".repeat(64),
    status: "scan_error",
    scan_attempt: 1,
    uploaded_at: "2026-11-01T00:01:00Z",
    scanned_at: null as string | null,
    failure_code: "scanner_unavailable" as string | null,
    created_at: "2026-11-01T00:00:00Z",
    updated_at: "2026-11-01T00:01:00Z",
    expires_at: "2026-11-01T00:30:00Z",
  };
  const retries: string[] = [];
  const revocations: string[] = [];
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method === "PUT" && url.includes("/retries/")) {
      retries.push(url);
      if (retries.length === 1)
        return Response.json({ error: "unavailable", message: "Uncertain retry" }, { status: 503 });
      row = {
        ...row,
        scan_attempt: 2,
        status: "available",
        failure_code: null,
        scanned_at: "2026-11-01T00:02:00Z",
        updated_at: "2026-11-01T00:02:00Z",
      };
      return Response.json(row);
    }
    if (init?.method === "PUT" && url.includes("/revocation?")) {
      revocations.push(url);
      row = { ...row, status: "revoked" };
      return Response.json(row);
    }
    return Response.json(page([row]));
  });
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <GovernanceEmploymentDocuments
        scope={scope}
        onBusy={() => {}}
        onClose={() => {}}
        onAccessLost={() => {}}
      />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Retry scan" }));
  await user.click(await screen.findByRole("button", { name: "Retry same file action" }));
  await screen.findByRole("button", { name: "Download" });
  expect(retries).toHaveLength(2);
  expect(retries[1]).toBe(retries[0]);
  expect(retries[0]).toContain("/retries/2?");
  await user.click(screen.getByRole("button", { name: "Revoke download" }));
  expect(revocations).toHaveLength(0);
  await user.click(screen.getByRole("button", { name: "Confirm revocation" }));
  await screen.findByText("revoked");
  expect(revocations).toHaveLength(1);
  expect(screen.queryByRole("button", { name: "Download" })).not.toBeInTheDocument();
});
it("stops before binary upload when registration loses the original authority", async () => {
  vi.stubGlobal("crypto", webcrypto);
  const lost = vi.fn();
  const writes: string[] = [];
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method === "PUT") {
      writes.push(url);
      return Response.json({ error: "forbidden", message: "Denied" }, { status: 403 });
    }
    return Response.json(page([]));
  });
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <GovernanceEmploymentDocuments
        scope={scope}
        onBusy={() => {}}
        onClose={() => {}}
        onAccessLost={lost}
      />
    </NavigationGuardProvider>,
  );
  await screen.findByText("No employment files prepared.");
  await user.upload(
    screen.getByLabelText("Employment evidence image"),
    new File(["x"], "synthetic-denied.png", { type: "image/png" }),
  );
  await user.click(screen.getByRole("button", { name: "Upload employment file" }));
  await screen.findByRole("alert");
  expect(lost).toHaveBeenCalledTimes(1);
  expect(writes).toHaveLength(1);
  expect(writes[0]).not.toContain("/content");
  expect(screen.queryByRole("button", { name: "Retry same file action" })).not.toBeInTheDocument();
});
