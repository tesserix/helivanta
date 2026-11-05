import * as contracts from "./staff-postings-contract";

const revision = "019a0000-0000-7000-8000-000000000001";
const immediate = {
  expected_revision_id: revision,
  expected_cutoff_version: 0,
  mode: "immediate",
  reason: " Synthetic responsibility ending ",
  sources_examined: true,
};

it("requires an exact approved revision and cutoff observation for an immediate ending", () => {
  const schema = contracts.StaffPostingCutoffInputContract;
  expect(schema).toBeDefined();
  expect(schema.parse(immediate)).toEqual({
    ...immediate,
    reason: "Synthetic responsibility ending",
  });
  for (const change of [
    { expected_revision_id: "00000000-0000-0000-0000-000000000000" },
    { expected_revision_id: "11111111-1111-4111-8111-111111111111" },
    { expected_cutoff_version: undefined },
    { expected_cutoff_version: -1 },
    { expected_cutoff_version: 9007199254740990 },
    { sources_examined: false },
    { reason: " " },
    { reason: "Synthetic\nreason" },
    { grants_access: true },
    { cutoff_at: "2027-01-01T00:00:00Z" },
  ]) {
    expect(schema.safeParse({ ...immediate, ...change }).success).toBe(false);
  }
});

it("requires a bounded scheduled instant without interpreting it as an immediate ending", () => {
  const schema = contracts.StaffPostingCutoffInputContract;
  const scheduled = { ...immediate, mode: "scheduled", cutoff_at: "2027-01-01T05:30:00+05:30" };
  expect(schema.parse(scheduled).cutoff_at).toBe(scheduled.cutoff_at);
  expect(schema.safeParse({ ...immediate, cutoff_at: null }).success).toBe(true);
  for (const cutoff_at of [
    undefined,
    null,
    "0001-01-01T00:00:00Z",
    "0000-01-01T00:00:00Z",
    "10000-01-01T00:00:00Z",
    "2027-01-01",
  ]) {
    expect(schema.safeParse({ ...scheduled, cutoff_at }).success, String(cutoff_at)).toBe(false);
  }
});

it("keeps the server instant immutable and relates cutoff versions in retained receipts", () => {
  const receipt = {
    id: "019a0000-0000-7000-8000-000000000002",
    hospital_id: "11111111-1111-4111-8111-111111111111",
    staff_id: "22222222-2222-4222-8222-222222222222",
    proposal_id: "019a0000-0000-7000-8000-000000000003",
    expected_revision_id: revision,
    expected_cutoff_version: 0,
    version: 1,
    mode: "immediate",
    requested_cutoff_at: null,
    cutoff_at: "2026-11-05T00:00:00.123456Z",
    recorded_at: "2026-11-05T00:00:00.123456Z",
    reason: "Synthetic retained cutoff",
    sources_examined: true,
  };
  const schema = contracts.StaffPostingCutoffContract;
  expect(schema.parse(receipt)).toEqual(receipt);
  for (const change of [
    { version: 2 },
    { id: receipt.hospital_id },
    { proposal_id: receipt.hospital_id },
    { cutoff_at: "2026-11-05T00:00:00.123457Z" },
    { requested_cutoff_at: receipt.cutoff_at },
    { cutoff_at: "0001-01-01T00:00:00Z", recorded_at: "0001-01-01T00:00:00Z" },
  ])
    expect(schema.safeParse({ ...receipt, ...change }).success).toBe(false);
  const scheduled = {
    ...receipt,
    mode: "scheduled",
    requested_cutoff_at: "2026-11-06T00:00:00Z",
    cutoff_at: "2026-11-06T00:00:00Z",
  };
  expect(schema.parse(scheduled)).toEqual(scheduled);
  expect(schema.safeParse({ ...scheduled, cutoff_at: receipt.cutoff_at }).success).toBe(false);
  expect(schema.safeParse({ ...scheduled, recorded_at: scheduled.cutoff_at }).success).toBe(false);
});
