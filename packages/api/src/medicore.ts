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

// Allergies and weight (#1373). The wire shape of
// backend/internal/modules/medicore/allergies.go and weights.go.
export const allergyReactions = [
  "rash",
  "urticaria",
  "angioedema",
  "anaphylaxis",
  "bronchospasm",
  "gastrointestinal",
  "other",
] as const;
export const allergySeverities = [
  "mild",
  "moderate",
  "severe",
  "life_threatening",
  "unknown",
] as const;
export const allergySources = ["patient_reported", "clinician_observed", "prior_record"] as const;
export const nonDrugCategories = [
  "food",
  "latex",
  "environmental",
  "contrast_media",
  "other",
] as const;

export type AllergyEntry = {
  id: string;
  version: number;
  status: "active" | "resolved" | "entered_in_error";
  substance_kind: "drug_molecule" | "drug_unlisted" | "non_drug";
  molecule_id: string | null;
  substance_label: string;
  non_drug_category: (typeof nonDrugCategories)[number] | null;
  reaction: (typeof allergyReactions)[number];
  reaction_note: string | null;
  severity: (typeof allergySeverities)[number];
  source: (typeof allergySources)[number];
  onset_date: string | null;
  recorded_by: string;
  recorded_at: string;
  needs_coding: boolean;
};

// "unknown" is what no record means; it is never a reassurance.
export type AllergyStatus = {
  status: "unknown" | "no_known_allergy" | "allergies_recorded";
  no_known_allergy: { id: string; recorded_by: string; recorded_at: string } | null;
  entries: AllergyEntry[];
  needs_coding: boolean;
  recording_enabled: boolean;
};

export type WeightObservation = {
  id: string;
  weight_kg: string;
  measured_at: string;
  method: "measured" | "estimated" | "reported";
  recorded_by: string;
  recorded_at: string;
  entered_in_error: boolean;
  age_days?: number;
};

// One drug master formulation from /drugmaster/search, as far as picking
// the molecule an allergy names needs.
export type DrugSearchResult = {
  formulation_id: string;
  label: string;
  ingredients: { molecule_id: string; inn_name: string }[];
};
