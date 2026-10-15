import { expect, it } from "vitest";
import { formatPharmacyMoney } from "./currency-amount-field";
import { CurrencyAmountField } from "./currency-amount-field";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
it("formats aggregate money without losing cents beyond Number.MAX_SAFE_INTEGER", () => {
  expect(formatPharmacyMoney("9007199254740993123", "INR")).toBe("INR 90,071,992,547,409,931.23");
  expect(formatPharmacyMoney(12525, "INR")).toBe("INR 125.25");
  expect(formatPharmacyMoney(125, "JPY")).toBe("JPY 125");
  expect(formatPharmacyMoney(12525, "KWD")).toBe("KWD 12.525");
});
it("preserves Pakistan paisa in displayed amounts and entered minor units", async () => {
  expect(formatPharmacyMoney(12525, "PKR")).toBe("PKR 125.25");
  const entered: number[] = [];
  const user = userEvent.setup();
  render(
    <CurrencyAmountField
      id="pkr"
      label="Amount"
      currency="PKR"
      initialMinor={0}
      onChange={(minor) => entered.push(minor)}
    />,
  );
  await user.clear(screen.getByLabelText("Amount (PKR)"));
  await user.type(screen.getByLabelText("Amount (PKR)"), "125.25");
  expect(entered.at(-1)).toBe(12525);
});
it("keeps large audit totals exact without converting strings to floats", () => {
  expect(formatPharmacyMoney("9007199254740993", "PKR")).toBe("PKR 90,071,992,547,409.93");
  expect(formatPharmacyMoney("-9007199254740993", "PKR")).toBe("PKR -90,071,992,547,409.93");
});
