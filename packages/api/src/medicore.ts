export { v7 as newEncounterId } from "uuid";

// An OPD encounter (#80 slice 1). The wire shape of
// backend/internal/modules/medicore/encounters.go.
export const encounterStatuses = [
  "arrived",
  "in_consultation",
  "closed",
  "left_without_being_seen",
  "cancelled",
] as const;
export type EncounterStatus = (typeof encounterStatuses)[number];
export type ConsultMode = "in_person" | "teleconsultation";
export type VisitType = "first" | "follow_up";

export type Encounter = {
  id: string;
  hospital_id: string;
  patient_id: string;
  consult_mode: ConsultMode;
  visit_type: VisitType;
  status: EncounterStatus;
  version: number;
  created_by: string;
  created_at: string;
  closed_at: string | null;
};

// The moves the server allows from each status; it re-checks them.
export const encounterMoves: Record<EncounterStatus, EncounterStatus[]> = {
  arrived: ["in_consultation", "left_without_being_seen", "cancelled"],
  in_consultation: ["closed"],
  closed: [],
  left_without_being_seen: [],
  cancelled: [],
};
