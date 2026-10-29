import { expect, type Browser, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { login, specAdmin, specPharmacist } from "./login";
import { uuidv7 } from "./pharmacy";
export { uuidv7 } from "./pharmacy";

export type Identity = { subject: string; tenant_id: string; data: string[] };
export type RegisteredPatient = { id: string; mrn: string };

export async function ownerAPI<T>(
  page: Page,
  method: string,
  path: string,
  data?: unknown,
): Promise<T> {
  const response = await page.request.fetch(`/api/v1${path}`, { method, data });
  expect(response.ok(), `${method} ${path}: ${await response.text()}`).toBe(true);
  return response.json() as Promise<T>;
}

export async function choose(page: Page, label: string, option: string | RegExp) {
  await page.getByRole("combobox", { name: label, exact: true }).click();
  await page.getByRole("option", { name: option, exact: typeof option === "string" }).click();
}

/** All setup uses the owning APIs. No projection/table fixture writes. */
export async function clinicalFixture(admin: Page, browser: Browser) {
  await login(admin, specAdmin());
  const administrator = await ownerAPI<Identity>(admin, "GET", "/iam/me/permissions");
  const context = await browser.newContext({ baseURL: new URL(admin.url()).origin });
  const clinician = await context.newPage();
  try {
    // The generic per-spec seed provides two identities. Add doctor to the
    // second through IAM; clinical actions never use the administrator.
    await login(clinician, specPharmacist());
    const identity = await ownerAPI<Identity>(clinician, "GET", "/iam/me/permissions");
    expect(identity.tenant_id).toBe(administrator.tenant_id);
    await ownerAPI(admin, "POST", "/iam/members", {
      subject: identity.subject,
      role_key: "doctor",
    });
    await expect
      .poll(
        async () => {
          const me = await ownerAPI<Identity>(clinician, "GET", "/iam/me/permissions");
          return (
            me.data.includes("medicore.allergy.record") && me.data.includes("medicore.visit.create")
          );
        },
        {
          timeout: 30_000,
          message: "IAM's granted doctor role reaches the real permission resolver",
        },
      )
      .toBe(true);

    const hospitalId = uuidv7();
    const hospitalName = `TEST OPD ${randomUUID().slice(0, 8)}`;
    const hospital = `/workforce/hospitals/${hospitalId}`;
    await ownerAPI(admin, "PUT", hospital, { name: hospitalName });
    await ownerAPI(
      admin,
      "PUT",
      `${hospital}/access/${encodeURIComponent(administrator.subject)}`,
      { level: "manager" },
    );
    await ownerAPI(admin, "PUT", `${hospital}/access/${encodeURIComponent(identity.subject)}`, {
      level: "reader",
    });
    const clinical = `/medicore/hospitals/${hospitalId}`;
    await ownerAPI(admin, "PUT", `${clinical}/access/${encodeURIComponent(identity.subject)}`);
    const registry = `/patients/hospitals/${hospitalId}/access`;
    await ownerAPI(admin, "PUT", `${registry}/${encodeURIComponent(administrator.subject)}`, {
      level: "registrar",
    });
    await ownerAPI(admin, "PUT", `${registry}/${encodeURIComponent(identity.subject)}`, {
      level: "reader",
    });
    const patientName = `TEST OPD Patient ${randomUUID().slice(0, 8)}`;
    const registration = await ownerAPI<{ patient: RegisteredPatient }>(
      admin,
      "POST",
      `/patients?hospital_id=${hospitalId}`,
      {
        request_id: randomUUID(),
        given_name: patientName,
        family_name: `SYNTHETIC-${randomUUID()}`,
        dob: "1990-01-01",
        sex: "other",
        notice_version: "TEST-E2E-1389",
        consented_by: "patient",
      },
    );
    const patient = registration.patient;
    expect(patient.id).toMatch(/^[0-9a-f-]{36}$/);
    expect(patient.mrn).toBeTruthy();
    const patientBase = `${clinical}/patients/${patient.id}`;
    await expect
      .poll(
        async () => {
          const response = await clinician.request.get(`/api/v1${patientBase}/allergies`);
          expect([200, 404]).toContain(response.status());
          return response.status();
        },
        {
          timeout: 30_000,
          message: "patientmaster registered.v1 reaches MediCore before the encounter is opened",
        },
      )
      .toBe(200);
    return {
      context,
      clinician,
      identity,
      administrator,
      hospitalId,
      hospitalName,
      hospital,
      clinical,
      patientBase,
      patient,
      patientName,
    };
  } catch (error) {
    await context.close();
    throw error;
  }
}
