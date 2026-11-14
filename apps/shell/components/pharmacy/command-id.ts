import { useRef } from "react";
import { newPharmacyId } from "@helivanta/api";

// A command's id lives as long as the values it was sent with: a retry of
// the same entry is the same command, so the server replays it (#1715); a
// changed entry is a new command and gets a new id.
export function useCommandId(): (values: unknown) => string {
  const last = useRef<{ key: string; id: string } | null>(null);
  return (values) => {
    const key = JSON.stringify(values);
    if (last.current?.key !== key) last.current = { key, id: newPharmacyId() };
    return last.current.id;
  };
}
