import { afterEach, expect, it, vi } from "vitest";
import { readAttachmentFile } from "./attachment-file";
import {
  fetchPharmacyTallyVouchers,
  pharmacyTallyLedgerNameSchema,
  readPharmacyTallyPreview,
  savePharmacyTallyLedgerMap,
} from "./pharmacy-tally-export";

const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199E000-0000-7000-8000-000000000001",
  subject: "user-owner",
};
const outlet = scope.outletId.toLowerCase();
const root = `/api/v1/pharmacy/pharmacies/${scope.pharmacyId}/outlets/${scope.outletId}/accounts/tally`;
const mapID = "0199e000-0000-7000-8000-000000000020";
const previous = "0199e000-0000-7000-8000-000000000019";
const period = { from: "2026-10-01", to: "2026-10-31" };
const body = "<ENVELOPE></ENVELOPE>\n";
const digest = "2ea16f429dc9038db4a7b05e8b5ff327c8352dc68cf3af3b2a0f47b897b0c545";
afterEach(() => vi.unstubAllGlobals());

type Seen = {
  url: string;
  method?: string;
  cache?: RequestCache;
  headers: Headers;
  body?: unknown;
};
function serve(reply: () => Response) {
  const seen: Seen[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit) => {
      seen.push({
        url,
        method: init.method,
        cache: init.cache,
        headers: new Headers(init.headers),
        body: typeof init.body === "string" ? JSON.parse(init.body) : undefined,
      });
      return reply();
    }),
  );
  return seen;
}
const json = (value: unknown) =>
  new Response(JSON.stringify(value), { headers: { "Content-Type": "application/json" } });

it("asks for a ledger name Tally will match exactly", () => {
  const message = (value: string) =>
    pharmacyTallyLedgerNameSchema.safeParse(value).error?.issues[0]?.message;
  expect(message("Sales & Returns <12%>")).toBeUndefined();
  expect(message("x".repeat(100))).toBeUndefined();
  expect(message("")).toBe("Enter the Tally ledger name.");
  expect(message("x".repeat(101))).toBe("Use at most 100 characters.");
  for (const spaced of [" Cash", "Cash ", "\u00a0Cash", "Cash\u3000"])
    expect(message(spaced)).toBe(
      "Remove the spaces before or after the name. Tally matches ledger names exactly.",
    );
  for (const control of ["Ca\u0007sh", "Cash\u0085x", "Ca\ufffesh", "Ca\uffffsh", "Ca\ud800sh"])
    expect(message(control)).toBe("Remove control characters from the name.");
});

const preview = {
  outlet_id: outlet,
  from: period.from,
  to: period.to,
  ledger_map_id: previous,
  sale_count: 2,
  credit_note_count: 1,
  supplier_payment_count: 1,
  payment_reversal_count: 0,
  purchase_count: 0,
  heads: [
    { head: "tender:cash", label: "Cash tender", ledger_name: "Cash", supplier_name: null },
    { head: "supplier:ACME", label: "Supplier ACME", ledger_name: null, supplier_name: "Acme" },
  ],
};

it("reads the preview for the outlet and period, pinned to the tenant", async () => {
  const seen = serve(() => json(preview));
  await expect(
    readPharmacyTallyPreview(scope, period, new AbortController().signal),
  ).resolves.toEqual(preview);
  expect(seen).toHaveLength(1);
  expect(seen[0]).toMatchObject({
    url: `${root}/preview?from=2026-10-01&to=2026-10-31&purchase_bills=true`,
    cache: "no-store",
  });
  expect(seen[0]?.headers.get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
});

it.each<[string, Record<string, unknown>]>([
  ["another outlet", { outlet_id: "0199e000-0000-7000-8000-000000000009" }],
  ["another first day", { from: "2026-10-02" }],
  ["another last day", { to: "2026-10-30" }],
  ["an unknown field", { total_minor: 1 }],
  ["a legacy preview without purchase counts", { purchase_count: undefined }],
  [
    "a repeated head",
    { heads: [preview.heads[0], { ...preview.heads[0], ledger_name: "Counter Cash" }] },
  ],
])("refuses a preview with %s", async (_, change) => {
  serve(() => json({ ...preview, ...change }));
  await expect(
    readPharmacyTallyPreview(scope, period, new AbortController().signal),
  ).rejects.toThrow("The Tally preview could not be verified. Reload and try again.");
});

const input = {
  replaces: previous,
  entries: [
    { head: "supplier:ACME", ledger_name: "Acme Pharma" },
    { head: "tender:cash", ledger_name: "Counter Cash" },
  ],
};
const receipt = {
  id: mapID,
  outlet_id: outlet,
  replaces: previous,
  entries: [
    { head: "supplier:ACME", label: "Supplier ACME", ledger_name: "Acme Pharma" },
    { head: "tender:cash", label: "Cash tender", ledger_name: "Counter Cash" },
    { head: "tender:upi", label: "UPI tender", ledger_name: "UPI" },
  ],
  recorded_by: scope.subject,
  recorded_at: "2026-11-21T10:00:00+05:30",
};

it("saves a ledger map version under the signed-in account", async () => {
  const seen = serve(() => json(receipt));
  await expect(
    savePharmacyTallyLedgerMap(scope, mapID, input, new AbortController().signal),
  ).resolves.toEqual(receipt);
  expect(seen[0]).toMatchObject({
    url: `${root}/ledger-maps/${mapID}`,
    method: "PUT",
    cache: "no-store",
    body: input,
  });
  expect(seen[0]?.headers.get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(seen[0]?.headers.get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});

it("saves the first version with no predecessor", async () => {
  const first = { replaces: null, entries: [input.entries[1]!] };
  const seen = serve(() => json({ ...receipt, replaces: null }));
  await savePharmacyTallyLedgerMap(scope, mapID, first, new AbortController().signal);
  expect(seen[0]?.body).toEqual(first);
});

it("refuses a name the server would refuse before sending it", async () => {
  const seen = serve(() => json(receipt));
  await expect(
    savePharmacyTallyLedgerMap(
      scope,
      mapID,
      { replaces: null, entries: [{ head: "tender:cash", ledger_name: "Cash " }] },
      new AbortController().signal,
    ),
  ).rejects.toThrow();
  expect(seen).toHaveLength(0);
});

it.each<[string, Record<string, unknown>]>([
  ["another id", { id: previous }],
  ["another outlet", { outlet_id: "0199e000-0000-7000-8000-000000000009" }],
  ["another predecessor", { replaces: null }],
  ["another account", { recorded_by: "user-other" }],
  [
    "a name other than the one sent",
    { entries: [{ ...receipt.entries[0], ledger_name: "Acme" }, receipt.entries[1]] },
  ],
  ["a sent head missing", { entries: [receipt.entries[0], receipt.entries[2]] }],
  ["a repeated head", { entries: [...receipt.entries, receipt.entries[2]] }],
])("refuses a ledger map receipt with %s", async (_, change) => {
  serve(() => json({ ...receipt, ...change }));
  await expect(
    savePharmacyTallyLedgerMap(scope, mapID, input, new AbortController().signal),
  ).rejects.toThrow("The ledger map receipt could not be verified. Reload before saving again.");
});

const filename = `pharmacy-tally-vouchers-${outlet}-2026-10-01-2026-10-31.xml`;
const file = (headers: Record<string, string> = {}) =>
  new Response(body, {
    headers: {
      "Content-Type": "application/xml; charset=utf-8",
      "Content-Disposition": `attachment; filename="${filename}"`,
      "X-Export-Purchase-Bills": "true",
      "X-Export-Voucher-Count": "4",
      "X-Export-Ledger-Map": mapID,
      "X-Export-Sha256": digest,
      ...headers,
    },
  });

it("downloads the vouchers for the previewed ledger map and verifies the bytes", async () => {
  const seen = serve(() => file());
  const signal = new AbortController().signal;
  const got = await fetchPharmacyTallyVouchers(scope, period, mapID.toUpperCase(), signal);
  expect(got.filename).toBe(filename);
  expect(got.vouchers).toBe(4);
  expect(new TextDecoder().decode(await readAttachmentFile(got.blob, signal))).toBe(body);
  expect(seen[0]).toMatchObject({
    url: `${root}/vouchers.xml?from=2026-10-01&to=2026-10-31&purchase_bills=true&ledger_map=${mapID}`,
    cache: "no-store",
  });
  expect(seen[0]?.headers.get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
});

it("asks for a file with no ledger map when the preview had none", async () => {
  const seen = serve(() =>
    file({
      "X-Export-Ledger-Map": "",
      "X-Export-Purchase-Bills": "true",
      "X-Export-Voucher-Count": "0",
    }),
  );
  const got = await fetchPharmacyTallyVouchers(scope, period, null, new AbortController().signal);
  expect(got.vouchers).toBe(0);
  expect(seen[0]?.url).toBe(
    `${root}/vouchers.xml?from=2026-10-01&to=2026-10-31&purchase_bills=true&ledger_map=`,
  );
});

it.each<Record<string, string>>([
  { "Content-Type": "text/html" },
  { "Content-Type": "text/xml" },
  {
    "Content-Disposition": `attachment; filename="pharmacy-tally-vouchers-${outlet}-2026-10-01-2026-10-30.xml"`,
  },
  { "Content-Disposition": `inline; filename="${filename}"` },
  { "Content-Disposition": `attachment; filename="../${filename}"` },
  { "Content-Disposition": "" },
  { "X-Export-Purchase-Bills": "true", "X-Export-Voucher-Count": "" },
  { "X-Export-Purchase-Bills": "true", "X-Export-Voucher-Count": "-1" },
  { "X-Export-Purchase-Bills": "true", "X-Export-Voucher-Count": "04" },
  { "X-Export-Purchase-Bills": "true", "X-Export-Voucher-Count": "5001" },
  { "X-Export-Ledger-Map": previous },
  { "X-Export-Ledger-Map": "" },
  { "X-Export-Sha256": "" },
  { "X-Export-Sha256": digest.toUpperCase() },
])("refuses a response that is not the requested Tally file: %o", async (headers) => {
  serve(() => file(headers));
  await expect(
    fetchPharmacyTallyVouchers(scope, period, mapID, new AbortController().signal),
  ).rejects.toThrow("The server did not return a valid Tally file. Try again.");
});

it("refuses a file whose bytes are not the ones the export audited", async () => {
  serve(() => file({ "X-Export-Sha256": "0".repeat(64) }));
  await expect(
    fetchPharmacyTallyVouchers(scope, period, mapID, new AbortController().signal),
  ).rejects.toThrow("The Tally file could not be verified. Try again.");
});

it("refuses a file if the workspace aborts while it is read", async () => {
  const controller = new AbortController();
  const response = file();
  const blob = await response.clone().blob();
  vi.spyOn(response, "blob").mockImplementation(async () => {
    controller.abort();
    return blob;
  });
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response));
  await expect(
    fetchPharmacyTallyVouchers(scope, period, mapID, controller.signal),
  ).rejects.toMatchObject({ name: "AbortError" });
});
