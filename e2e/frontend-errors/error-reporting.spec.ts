import { readFileSync } from "node:fs";
import { expect, test, type Request } from "@playwright/test";
import {
  CLIENT_ERROR_EVENT,
  CLIENT_ERROR_PATH,
  frontendErrorReportContract,
} from "../../apps/shell/lib/frontend-errors-contract";

const privateMarker = "synthetic-private-error-1706";
const origin = process.env.HELIVANTA_ERROR_BROWSER_URL!;

function matchingLog(reference: string) {
  return readFileSync(process.env.HELIVANTA_ERROR_WEB_LOG!, "utf8")
    .split("\n")
    .flatMap((line) => {
      try {
        const row = JSON.parse(line);
        return row.event === CLIENT_ERROR_EVENT && row.reference === reference ? [row] : [];
      } catch {
        return [];
      }
    });
}

async function verifyWire(request: Request, reference: string) {
  const report = frontendErrorReportContract.parse(request.postDataJSON());
  expect(report.reference).toBe(reference);
  expect(report.area).toBe("ambulance");
  expect(JSON.stringify(report)).not.toContain(privateMarker);
  const headers = await request.allHeaders();
  expect(headers.origin).toBe(new URL(origin).origin);
  expect(headers.cookie).toBeUndefined();
  expect(headers.referer).toBeUndefined();
  await expect.poll(() => matchingLog(reference).length).toBeGreaterThan(0);
  expect(JSON.stringify(matchingLog(reference))).not.toContain(privateMarker);
}

test("production boundary conceals a render failure, reports its reference and retries", async ({
  page,
  context,
}) => {
  await context.addCookies([
    { name: "diagnostic_privacy_fixture", value: privateMarker, url: origin },
  ]);
  await page.goto(`/ambulance?synthetic_private=${privateMarker}`);
  const choice = page.getByRole("combobox");
  await expect(choice).toBeVisible();
  const requests: Request[] = [];
  page.on("request", (request) => {
    if (new URL(request.url()).pathname === CLIENT_ERROR_PATH) requests.push(request);
  });
  await page.evaluate((marker) => {
    const original = Element.prototype.setAttribute;
    const failure = new Error(marker);
    const state = window as typeof window & { restoreDiagnosticFixture?: () => void };
    state.restoreDiagnosticFixture = () => {
      Element.prototype.setAttribute = original;
    };
    Element.prototype.setAttribute = function (name, value) {
      if (name === "role" && value === "listbox") throw failure;
      return original.call(this, name, value);
    };
  }, privateMarker);
  try {
    await choice.click();
    await expect(page.getByRole("heading", { name: "Something went wrong" })).toBeVisible();
    await expect(page.locator("body")).not.toContainText(privateMarker);
    const reference = await page.locator("code").filter({ hasText: /^HV-/ }).innerText();
    await expect
      .poll(() => requests.some((request) => request.postDataJSON().reference === reference))
      .toBe(true);
    const reported = requests.find((request) => request.postDataJSON().reference === reference)!;
    expect((await reported.response())?.status()).toBe(204);
    await verifyWire(reported, reference);
    await page.evaluate(() => {
      const state = window as typeof window & { restoreDiagnosticFixture?: () => void };
      state.restoreDiagnosticFixture?.();
      delete state.restoreDiagnosticFixture;
    });
    await page.getByRole("button", { name: "Try again", exact: true }).click();
    await expect(page.getByRole("combobox")).toBeVisible();
    await expect(page.getByRole("heading", { name: "Something went wrong" })).toHaveCount(0);
  } finally {
    await page.evaluate(() => {
      const state = window as typeof window & { restoreDiagnosticFixture?: () => void };
      state.restoreDiagnosticFixture?.();
      delete state.restoreDiagnosticFixture;
    });
  }
});

test("startup observes unhandled rejections and emits only bounded safe metadata", async ({
  page,
}) => {
  await page.goto("/ambulance");
  await expect(page.getByRole("combobox")).toBeVisible();
  const delivery = page.waitForResponse(
    (response) => new URL(response.url()).pathname === CLIENT_ERROR_PATH,
  );
  await page.evaluate((marker) => {
    void Promise.reject(new Error(marker));
  }, privateMarker);
  const response = await delivery;
  expect(response.status()).toBe(204);
  const report = frontendErrorReportContract.parse(response.request().postDataJSON());
  expect(report.source).toBe("unhandled_rejection");
  await verifyWire(response.request(), report.reference);
});
