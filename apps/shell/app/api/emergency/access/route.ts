import { emergencyAccess } from "@helivanta/emergency-access";

const headers = { "Cache-Control": "no-store", "Referrer-Policy": "no-referrer" };

export function GET(request: Request) {
  const query = new URL(request.url).searchParams;
  const countries = query.getAll("country");
  if (
    [...query.keys()].some((key) => key !== "country") ||
    countries.length > 1 ||
    (countries.length === 1 && countries[0] !== "IN" && countries[0] !== "OTHER")
  ) {
    return Response.json(
      { error: "invalid_query", message: "Choose a supported country." },
      { status: 400, headers },
    );
  }
  return Response.json(emergencyAccess(countries[0] ?? "OTHER"), { headers });
}
