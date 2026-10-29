import { expect, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { choose, clinicalFixture, ownerAPI, uuidv7 } from "./support/opd";

type Encounter = { id: string; patient_id: string; hospital_id: string; status: string };
type AllergyState = {
  status: string;
  recording_enabled: boolean;
  no_known_allergy: { recorded_by: string } | null;
  entries: {
    substance_kind: string;
    molecule_id: string;
    needs_coding: boolean;
    recorded_by: string;
  }[];
};

test("clinician opens an OPD encounter and records patient safety through the real owners", async ({
  page,
  browser,
}) => {
  test.setTimeout(120_000);
  const fixture = await clinicalFixture(page, browser);
  const {
    clinician,
    identity,
    hospitalId,
    hospitalName,
    patient,
    patientName,
    clinical,
    patientBase,
  } = fixture;
  try {
    await clinician.goto("/medicore/opd");
    // This new hospital is the clinician's only workforce directory grant.
    await expect(
      clinician.getByRole("heading", { name: `OPD encounters at ${hospitalName}` }),
    ).toBeVisible();
    await clinician.getByLabel("Find a patient registered here", { exact: true }).fill(patient.mrn);
    await clinician.getByRole("button", { name: "Search", exact: true }).click();
    await choose(clinician, "Patient", new RegExp(patient.mrn));
    const opened = clinician.waitForResponse(
      (response) =>
        response.request().method() === "PUT" && response.url().includes(`${clinical}/encounters/`),
    );
    await clinician.getByRole("button", { name: "Open encounter", exact: true }).click();
    const openedResponse = await opened;
    expect(openedResponse.status()).toBe(201);
    const encounter = (await openedResponse.json()) as Encounter;
    expect(encounter.patient_id).toBe(patient.id);
    expect(encounter.hospital_id).toBe(hospitalId);
    expect(encounter.status).toBe("arrived");
    let row = clinician
      .getByRole("list", { name: "Encounters", exact: true })
      .getByRole("listitem")
      .filter({ hasText: patientName });
    await expect(row.getByText("Arrived", { exact: true })).toBeVisible();
    await row.getByRole("button", { name: "Start consultation", exact: true }).click();
    await expect(row.getByText("In consultation", { exact: true })).toBeVisible();
    await clinician.reload();
    row = clinician
      .getByRole("list", { name: "Encounters", exact: true })
      .getByRole("listitem")
      .filter({ hasText: patientName });
    await expect(row.getByText("In consultation", { exact: true })).toBeVisible();
    const persisted = await ownerAPI<Encounter>(
      clinician,
      "GET",
      `${clinical}/encounters/${encounter.id}`,
    );
    expect(persisted.status).toBe("in_consultation");
    await row.getByRole("button", { name: "Patient safety", exact: true }).click();
    await expect(row.getByText("Allergies unknown: not yet asked", { exact: true })).toBeVisible();
    await expect(row.getByText("No weight recorded.", { exact: true })).toBeVisible();
    const before = await ownerAPI<AllergyState>(clinician, "GET", `${patientBase}/allergies`);
    expect(before.status).toBe("unknown");
    expect(
      before.recording_enabled,
      "the synthetic e2e API must explicitly enable the recording fixture",
    ).toBe(true);
    await row.getByRole("button", { name: "No known allergies", exact: true }).click();
    await row.getByRole("button", { name: "Confirm no known allergies", exact: true }).click();
    await expect(
      row.getByText(`No known allergies, recorded by ${identity.subject}`, { exact: false }),
    ).toBeVisible();
    const noKnown = await ownerAPI<AllergyState>(clinician, "GET", `${patientBase}/allergies`);
    expect(noKnown.status).toBe("no_known_allergy");
    expect(noKnown.no_known_allergy?.recorded_by).toBe(identity.subject);

    await row.getByRole("button", { name: "Record allergy", exact: true }).click();
    await row.getByLabel("Find the drug", { exact: true }).fill("TEST E2E molecule");
    await row.getByRole("button", { name: "Search drugs", exact: true }).click();
    await choose(clinician, "Drug", "TEST E2E molecule");
    await choose(clinician, "Reaction", "Rash");
    await choose(clinician, "Severity", "Mild");
    const recorded = clinician.waitForResponse(
      (response) =>
        response.request().method() === "PUT" &&
        response.url().includes(`${patientBase}/allergies/`),
    );
    await row.getByRole("button", { name: "Save allergy", exact: true }).click();
    const recordedResponse = await recorded;
    expect(recordedResponse.ok()).toBe(true);
    await expect(
      row
        .getByRole("list", { name: "Active allergies" })
        .getByText("TEST E2E molecule", { exact: true }),
    ).toBeVisible();
    await expect(row.getByText(/No known allergies, recorded by/)).toHaveCount(0);
    const allergy = await ownerAPI<AllergyState>(clinician, "GET", `${patientBase}/allergies`);
    expect(allergy.status).toBe("allergies_recorded");
    expect(allergy.entries).toHaveLength(1);
    expect(allergy.entries[0]?.substance_kind).toBe("drug_molecule");
    expect(allergy.entries[0]?.molecule_id).toMatch(/^[0-9a-f-]{36}$/);
    expect(allergy.entries[0]?.needs_coding).toBe(false);
    expect(allergy.entries[0]?.recorded_by).toBe(identity.subject);

    await row.getByRole("button", { name: "Record weight", exact: true }).click();
    await row.getByRole("textbox", { name: "Weight", exact: true }).fill("62.125");
    await row.getByRole("button", { name: "Save weight", exact: true }).click();
    await expect(row.getByText("62.125 kg (measured), today", { exact: true })).toBeVisible();
    const weight = await ownerAPI<{
      weight_kg: string;
      age_days: number;
      recorded_by: string;
      encounter_id: string;
    }>(clinician, "GET", `${patientBase}/weights/latest`);
    expect(weight.weight_kg).toBe("62.125");
    expect(weight.age_days).toBe(0);
    expect(weight.recorded_by).toBe(identity.subject);
    expect(weight.encounter_id).toBe(encounter.id);
    await clinician.reload();
    await row.getByRole("button", { name: "Patient safety", exact: true }).click();
    await expect(row.getByRole("list", { name: "Active allergies" })).toContainText(
      "TEST E2E molecule",
    );
    await expect(row.getByText("62.125 kg (measured), today", { exact: true })).toBeVisible();
  } finally {
    await fixture.context.close();
  }
});

test("a clinician without a MediCore hospital grant sees no scoped encounters", async ({
  page,
  browser,
}) => {
  test.setTimeout(90_000);
  const fixture = await clinicalFixture(page, browser);
  try {
    const hospital = uuidv7();
    const name = `TEST No Clinical Access ${randomUUID().slice(0, 8)}`;
    await ownerAPI(page, "PUT", `/workforce/hospitals/${hospital}`, { name });
    await ownerAPI(
      page,
      "PUT",
      `/workforce/hospitals/${hospital}/access/${encodeURIComponent(fixture.identity.subject)}`,
      { level: "reader" },
    );
    await fixture.clinician.goto("/medicore/opd");
    await choose(fixture.clinician, "Hospital", name);
    await expect(
      fixture.clinician.getByRole("heading", {
        name: `No MediCore access at ${name}`,
        exact: true,
      }),
    ).toBeVisible();
    await expect(
      fixture.clinician.getByRole("button", { name: "Open encounter", exact: true }),
    ).toHaveCount(0);
    await expect(
      fixture.clinician.getByRole("list", { name: "Encounters", exact: true }),
    ).toHaveCount(0);
    const denied = await fixture.clinician.request.get(
      `/api/v1/medicore/hospitals/${hospital}/encounters`,
    );
    expect(denied.status()).toBe(404);
    const body = await denied.json();
    expect(body.error).toBe("not_found");
    expect(body.data).toBeUndefined();
  } finally {
    await fixture.context.close();
  }
});
