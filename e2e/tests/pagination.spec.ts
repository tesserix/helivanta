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

  // Walk until this run's own visits are all on screen.
  //
  // Deliberately NOT "walk until the button disappears". The tenant is
  // shared with every other spec and the dev database persists, so this
  // spec's 55 visits land on top of however many thousand already exist —
  // exhausting the tenant would take a growing number of pages every run
  // and eventually cannot finish at all. An earlier version of this test
  // did exactly that: it passed at ~180 rows, then failed at ~800 with the
  // button still visible after 14 clicks, which is the test degrading
  // rather than the contract breaking.
  //
  // This run's visits are the newest, so created_at DESC puts all 55
  // within the first pages; patient(0) is the oldest of them and therefore
  // the last to appear. Reaching it is the real statement — the walk got
  // past page one and kept its place across page boundaries.
  //
  // The complementary property — that the control DISAPPEARS at the true
  // end of a collection — is proven where it can be stated exactly:
  // packages/ui/src/load-more.test.tsx (renders nothing when hasMore is
  // false) and the per-module Go tests, which walk a tenant they own to
  // exhaustion and assert has_more goes false with a null cursor.
  const oldestOfRun = visits.getByText(patient(0), { exact: true });
  for (let i = 0; i < MAX_PAGES && !(await oldestOfRun.isVisible()); i++) {
    await expect(loadMore).toBeVisible();
    const before = await rows.count();
    await loadMore.click();
    await expect.poll(() => rows.count()).toBeGreaterThan(before);
  }
  await expect(oldestOfRun).toBeVisible();

  // More than one page was genuinely required: 55 seeded rows cannot fit
  // in a 50-row page, so a test that somehow saw them all at once would be
  // looking at the pre-#816 behaviour wearing a cursor.
  expect(await rows.count()).toBeGreaterThan(PAGE_SIZE);

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
