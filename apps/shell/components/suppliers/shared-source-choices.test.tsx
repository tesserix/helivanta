import { render, screen, fireEvent } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { SharedSourceChoices } from "./shared-source-choices";
import type { PartnerShipmentSourceChoice } from "@helivanta/api";
const id = (n: number) => `0199e000-0000-7000-8000-${n.toString().padStart(12, "0")}`;
const choice: PartnerShipmentSourceChoice = {
  recipientKind: "group",
  source: {
    grant_id: id(1),
    relationship_id: id(2),
    terms_sha256: "a".repeat(64),
    scope: {
      source_tenant_id: id(3),
      recipient_tenant_id: id(4),
      hospital_id: id(5),
      source_product: "pharmacy",
      source_organization_id: id(6),
      source_location_id: id(7),
    },
    source_organization_name: "TEST Independent Pharmacy",
    source_location_name: "TEST Nagpur Branch",
  },
};
it("shows only current source names and sends the selected scoped choice", () => {
  const select = vi.fn();
  const view = render(<SharedSourceChoices choices={[choice]} onSelect={select} />);
  expect(screen.getByText("TEST Independent Pharmacy")).toBeVisible();
  expect(screen.getByText("TEST Nagpur Branch")).toBeVisible();
  expect(screen.getByText("Team access")).toBeVisible();
  expect(view.container.textContent).not.toContain(id(1));
  expect(view.container.textContent).not.toContain(id(6));
  fireEvent.click(
    screen.getByRole("button", {
      name: "View shipments from TEST Independent Pharmacy — TEST Nagpur Branch",
    }),
  );
  expect(select).toHaveBeenCalledWith(choice);
});
