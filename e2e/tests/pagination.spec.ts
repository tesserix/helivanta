import { expect, test } from "@playwright/test";
import { login } from "./support/login";

// #816: a ward list longer than one page must be readable to completion.
//
// Before this contract, GET /v1/medicore/visits returned a hardcoded
// LIMIT 100 with no cursor and no marker, so a busy OPD showed exactly 100
// visits and said nothing about the rest. This spec is the end-to-end
// statement that the defect is gone: the list pages at a known size, the
// "Load more" control appears precisely because there is more, and walking
// it reaches every visit exactly once.
//
// The seeded visits carry a per-run stamp because the dev database is
// shared with the other specs and persists between runs — an unstamped
// "Page Patient 0" would match leftovers from a previous run and the exact
// text lookups below would fail Playwright's strict mode rather than the
// assertion they exist to make.

const PAGE_SIZE = 50; // pagination.DefaultLimit
const SEEDED = 55; // one page plus a remainder, so page 2 is non-trivial
const MAX_PAGES = 20; // a bound, not an expectation: the tenant's own rows count too

test("a ward with more than one page reads to completion", async ({ page }) => {
  // Creating 55 visits through the UI's own form is deliberately the slow
  // path — it is what a clerk does, and it is the only way the seeded rows
  // travel the same handler the assertion later reads back through. That
  // does not fit the 60s default.
  test.setTimeout(300_000);

  await login(page);
  await page.goto("/medicore/opd");
  await page.waitForLoadState("networkidle");

  const visits = page
    .locator("section")
    .filter({ has: page.getByRole("heading", { name: "Visits" }) });
  const rows = visits.locator("li");
  // Scoped to the visits panel: the ping panel on the same page renders its
  // own "Load more", and an unscoped lookup would be ambiguous.
  const loadMore = visits.getByRole("button", { name: "Load more" });

  const stamp = Date.now();
  // Zero-padded so no name is a prefix of another: an unpadded "-1" is a
  // substring of "-10".."-19", and the exact-once count below would read
  // eleven matches as eleven duplicates.
  const patient = (i: number) =>
    `Page Patient ${stamp}-${String(i).padStart(2, "0")}`;

  const nameField = page.getByLabel("Patient name");
  for (let i = 0; i < SEEDED; i++) {
    await nameField.fill(patient(i));
    await page.getByRole("button", { name: "Create visit" }).click();
    // The form resets only on a successful mutation, so an empty field is
    // the signal the visit actually landed. Firing the next click without
    // waiting would race the disabled button and silently drop creates.
    await expect(nameField).toHaveValue("");
  }

  // The first page is exactly one page — not "about" one page. This is the
  // assertion that fails if the page size is ever raised to the point where
  // the collection fits in one response, which is the pre-#816 behaviour
  // wearing a cursor.
  await expect(rows).toHaveCount(PAGE_SIZE);
  await expect(loadMore).toBeVisible();

  // Walk to the end. The control disappearing is the positive statement
  // that the client now holds the whole collection — the statement a
  // silently-truncated list could never make.
  for (let i = 0; i < MAX_PAGES && (await loadMore.isVisible()); i++) {
    const before = await rows.count();
    await loadMore.click();
    await expect.poll(() => rows.count()).toBeGreaterThan(before);
  }
  await expect(loadMore).toBeHidden();

  // The first visit created is the oldest of the run and therefore last in
  // the created_at DESC ordering. Seeing it proves the walk reached past
  // the first page rather than merely rendering a longer first one.
  await expect(visits.getByText(patient(0), { exact: true })).toBeVisible();

  // Exact-once, through the UI: keyset paging must neither repeat a row as
  // an insert shifts positions nor skip one. A count of 2 here is the
  // duplicate-row symptom the offset alternative produces.
  const rendered = await rows.allInnerTexts();
  for (let i = 0; i < SEEDED; i++) {
    const name = patient(i);
    const seen = rendered.filter((text) => text.includes(name)).length;
    expect(seen, `${name} appeared ${seen} times across the pages`).toBe(1);
  }
});
