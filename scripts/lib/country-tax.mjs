// A tax scheme describes structure only: who registers, how products are
// classified and how a jurisdiction splits tax. It never carries a rate.
export function validateTax(p) {
  if (p.tax === null) return;
  const t = p.tax;
  const fail = (why) => {
    throw new Error(`Invalid tax scheme for ${p.code}: ${why}`);
  };
  if (!t || typeof t !== "object") fail("tax must be an object or null");
  if (t.scheme !== "gst") fail("unsupported scheme");
  if (!p.tax_identifier_kinds.includes(t.registration_kind))
    fail("registration_kind is not a tax identifier kind");
  if (!/^[A-Z]{2,10}$/.test(t.classification?.name ?? "")) fail("classification name");
  try {
    new RegExp(t.classification.pattern);
  } catch {
    fail("classification pattern does not compile");
  }
  if (!t.classification.pattern.startsWith("^") || !t.classification.pattern.endsWith("$"))
    fail("classification pattern must be anchored");
  if (!Array.isArray(t.jurisdictions) || t.jurisdictions.length === 0) fail("no jurisdictions");
  if (new Set(t.jurisdictions.map((j) => j.code)).size !== t.jurisdictions.length)
    fail("duplicate jurisdiction code");
  for (const j of t.jurisdictions)
    if (
      !/^[0-9]{2}$/.test(j.code) ||
      !["state", "union_territory"].includes(j.kind) ||
      typeof j.name !== "string" ||
      j.name.trim() === ""
    )
      fail("jurisdiction " + j.code);
  if (!Array.isArray(t.sources) || t.sources.length === 0) fail("no sources");
  for (const src of t.sources)
    if (!/^https:\/\//.test(src.url) || !/^\d{4}-\d{2}-\d{2}$/.test(src.reviewed_at) || !src.topic)
      fail("source " + src.url);
}
