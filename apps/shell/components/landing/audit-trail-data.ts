import type { ModuleName } from "@/app/platform-catalog";

export type TrailLane = "stock" | "money" | "audit";

export type TrailEntry = {
  id: string;
  lane: TrailLane;
  actor: string;
  title: string;
  figure: string;
  text: string;
  corrects?: string;
  readOnly?: boolean;
};

export const trail: readonly TrailEntry[] = [
  {
    id: "receipt",
    lane: "stock",
    actor: "Pharmacy store",
    title: "Goods received",
    figure: "+40 boxes, batch B-2291",
    text: "Forty boxes arrive from the supplier. Batch and expiry are recorded, and the stock waits in quarantine.",
  },
  {
    id: "invoice",
    lane: "money",
    actor: "Accounts",
    title: "Supplier invoice",
    figure: "Payable ₹18,400",
    text: "The supplier's invoice becomes a payable, linked to the receipt it bills for.",
  },
  {
    id: "release",
    lane: "stock",
    actor: "Pharmacist",
    title: "Released for use",
    figure: "40 boxes usable",
    text: "A pharmacist checks the batch and releases it from quarantine for dispensing.",
  },
  {
    id: "dispense",
    lane: "stock",
    actor: "Pharmacy counter",
    title: "Dispensed",
    figure: "−1 box, batch B-2291",
    text: "A box is dispensed against a prescription, and the batch goes on the patient's record.",
  },
  {
    id: "payment",
    lane: "money",
    actor: "Finance",
    title: "Supplier paid",
    figure: "Settled ₹18,400",
    text: "Finance pays the supplier. The payment settles the payable and leaves the invoice entry as it was.",
  },
  {
    id: "correction",
    lane: "stock",
    actor: "Store keeper",
    title: "Count correction",
    figure: "−2 boxes, reason recorded",
    text: "A stock count finds two boxes short. A correction is added with its reason; the receipt keeps its original forty.",
    corrects: "receipt",
  },
  {
    id: "audit",
    lane: "audit",
    actor: "Auditor",
    title: "Audit review",
    figure: "Reads every entry, changes none",
    text: "An auditor with reader access follows every entry from receipt to payment and can export them. Nothing in the trail can be changed, and each export is itself logged.",
    readOnly: true,
  },
];

export const trailModules: readonly ModuleName[] = ["Pharmacy management", "Finance & accounts"];

export const trailNote =
  "Available today in the pharmacy: receipts with batch and expiry, quarantine, count corrections, supplier payables and settlements, and reader access with logged exports. Release from quarantine, dispensing from released stock and hospital-wide finance are planned.";
