import type { PharmacyHospitalAffiliation } from "@helivanta/api";
import type { SelectOption } from "@helivanta/ui";

/**
 * One picker option per verified hospital (#1637). `hospital_name` is only
 * meaningful on a verified affiliation. Two distinct hospitals with the same
 * name get the id appended so the operator can tell them apart; a missing name
 * (null, or absent from an older server) says so rather than guessing.
 */
export function hospitalOptions(affiliations: PharmacyHospitalAffiliation[]): SelectOption[] {
  const names = new Map<string, string | null>();
  for (const a of affiliations)
    if (a.verification_status === "verified" && !names.has(a.hospital_id))
      names.set(a.hospital_id, a.hospital_name ?? null);
  const count = new Map<string, number>();
  for (const n of names.values()) if (n) count.set(n, (count.get(n) ?? 0) + 1);
  return [...names].map(([id, name]) => ({
    value: id,
    label: !name
      ? `Name unavailable · ${id}`
      : (count.get(name) ?? 0) > 1
        ? `${name} · ${id}`
        : name,
  }));
}
