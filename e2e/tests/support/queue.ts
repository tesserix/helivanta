// Past dispenses' Waiting tab (the retired queue, #1680) is oldest first and
// paged (#1295), so specs open it searched to their own patient (D9).
export const waitingFor = (patient: string) =>
  `/pharmacy/dispense?tab=waiting&q=${encodeURIComponent(patient)}`;
