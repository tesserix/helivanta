export function globalApiURL(value: string | undefined, production: boolean): string {
  if (value === undefined && !production) return "http://localhost:8081";
  if (!value) throw new Error("API_URL must point to the Helivanta Global API");
  const url = new URL(value);
  if (
    !["http:", "https:"].includes(url.protocol) ||
    url.username ||
    url.password ||
    url.search ||
    url.hash ||
    (url.pathname !== "/" && url.pathname !== "")
  )
    throw new Error("API_URL must be a Global API origin without credentials, path or query");
  return url.origin;
}
