import { expect, test } from "@playwright/test";
import { choose, readyOutlet } from "./support/pharmacy-counter";
import { affiliatedPatient } from "./support/pharmacy-reports";

// #1637: the counter's Hospital picker lists a verified hospital by name, not
// by id (an exact match on the option text also rules out an appended id).
// Workforce announces the name every 30 s, so poll for it.

test("the counter's Hospital picker shows the hospital's name, not its id", async ({
  page,
  browser,
}) => {
  test.setTimeout(180_000);
  const fixture = await readyOutlet(page, browser);
  const { cashier, outlet } = fixture;
  try {
    const patient = await affiliatedPatient(page, cashier, outlet);
    await expect
      .poll(
        async () => {
          await cashier.goto("/pharmacy/counter");
          await choose(cashier, "Customer", "Registered patient");
          await cashier.getByRole("combobox", { name: "Hospital", exact: true }).click();
          const options = await cashier.getByRole("option").allTextContents();
          return options.find((o) => o.includes(patient.hospitalName)) ?? options;
        },
        { timeout: 60_000, message: "the announced hospital name reaches the picker" },
      )
      .toBe(patient.hospitalName);
  } finally {
    await fixture.context.close();
  }
});
