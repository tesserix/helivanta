import {
  FileCheck,
  FilePen,
  House,
  Pill,
  ReceiptText,
  Stethoscope,
  TestTube,
  UserPlus,
  type LucideIcon,
} from "lucide-react";
import type { ModuleName } from "@/app/platform-catalog";

export type Station = {
  id: string;
  label: string;
  owner: string;
  record: string;
  text: string;
  module: ModuleName;
  icon: LucideIcon;
};

export const patient = {
  name: "Asha",
  summary: "Asha, 46, comes to outpatients with a cough that will not settle.",
  number: "HV-204817",
};

export const stations: readonly Station[] = [
  {
    id: "registration",
    label: "Registration",
    owner: "Front desk",
    record: "Registered",
    text: "Registered once, with one hospital number. On the next visit Asha is found, not registered again.",
    module: "Patient management",
    icon: UserPlus,
  },
  {
    id: "consultation",
    label: "Consultation",
    owner: "Doctor",
    record: "OPD visit opened",
    text: "The consulting doctor opens an outpatient visit. Everything that follows attaches to it.",
    module: "Outpatient & inpatient care",
    icon: Stethoscope,
  },
  {
    id: "lab-order",
    label: "Lab order",
    owner: "Doctor",
    record: "Blood test ordered",
    text: "The doctor orders a blood test against the visit, and it appears on the laboratory's worklist.",
    module: "Laboratory management",
    icon: TestTube,
  },
  {
    id: "result",
    label: "Result",
    owner: "Laboratory",
    record: "Result released",
    text: "The laboratory reports against the same order, so the result returns to the doctor who asked for it.",
    module: "Laboratory management",
    icon: FileCheck,
  },
  {
    id: "prescription",
    label: "Prescription",
    owner: "Doctor",
    record: "Medicines prescribed",
    text: "The doctor prescribes from the visit, so the pharmacy reads the prescription instead of a handwritten slip.",
    module: "Clinical records & specialties",
    icon: FilePen,
  },
  {
    id: "dispensing",
    label: "Dispensing",
    owner: "Pharmacy",
    record: "Dispensed, batch recorded",
    text: "The pharmacy dispenses and records which batch went to Asha, so a recall can find every patient.",
    module: "Pharmacy management",
    icon: Pill,
  },
  {
    id: "billing",
    label: "Billing",
    owner: "Billing desk",
    record: "One bill raised",
    text: "Consultation, test and medicines arrive on one bill or one insurance claim, with nothing typed twice.",
    module: "Billing & insurance",
    icon: ReceiptText,
  },
  {
    id: "discharge",
    label: "Discharge and follow-up",
    owner: "Doctor",
    record: "Summary signed, follow-up booked",
    text: "The doctor signs a summary and books the follow-up. The next visit starts from this record.",
    module: "Appointments & reception",
    icon: House,
  },
];

export const journeyNote =
  "Registration, visits, lab orders and dispensing are available as foundations. Validated result release, prescriptions, hospital billing and follow-up booking are planned.";
