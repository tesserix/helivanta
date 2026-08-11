// Seeds the GIP emulator: one user with a tenant_id custom claim.
// Usage: node scripts/seed-dev.mjs   (emulator must be running)
const HOST = process.env.AUTH_EMULATOR_HOST ?? "localhost:9099";
const PROJECT = process.env.GIP_PROJECT_ID ?? "demo-hms";
const TENANT_ID = "11111111-1111-1111-1111-111111111111";
const EMAIL = "test@hms.dev";
const PASSWORD = "password123";

const base = `http://${HOST}/identitytoolkit.googleapis.com/v1`;
const headers = {
  "Content-Type": "application/json",
  Authorization: "Bearer owner",
};

async function main() {
  const signUp = await fetch(`${base}/accounts:signUp?key=demo-key`, {
    method: "POST",
    headers,
    body: JSON.stringify({
      email: EMAIL,
      password: PASSWORD,
      returnSecureToken: true,
    }),
  });
  const created = await signUp.json();
  if (!signUp.ok && created?.error?.message !== "EMAIL_EXISTS") {
    throw new Error(`signUp failed: ${JSON.stringify(created)}`);
  }
  let localId = created.localId;
  if (!localId) {
    const lookup = await fetch(
      `http://${HOST}/emulator/v1/projects/${PROJECT}/accounts:query`,
      { method: "POST", headers, body: JSON.stringify({}) },
    ).then((r) => r.json());
    localId = lookup.userInfo?.find((u) => u.email === EMAIL)?.localId;
  }
  if (!localId) throw new Error("could not resolve user id");

  const update = await fetch(`${base}/accounts:update?key=demo-key`, {
    method: "POST",
    headers,
    body: JSON.stringify({
      localId,
      customAttributes: JSON.stringify({ tenant_id: TENANT_ID }),
    }),
  });
  if (!update.ok) throw new Error(`set claims failed: ${await update.text()}`);
  console.log(`Seeded ${EMAIL} / ${PASSWORD} with tenant_id=${TENANT_ID}`);

  // Bootstrap: the first tenant_admin cannot be granted through a route
  // that requires iam.member.manage, so the seed writes the membership row
  // directly. The API's reconciler turns it into tuples on next boot; the
  // iam-fga-sync consumer does it immediately for later grants.
  const { Client } = await import("pg");
  const pg = new Client({
    connectionString:
      process.env.ADMIN_DATABASE_URL ??
      "postgres://hms:hms@localhost:5432/hms?sslmode=disable",
  });
  await pg.connect();
  await pg.query(
    `INSERT INTO iam_members (tenant_id, subject, role_key)
     VALUES ($1, $2, 'tenant_admin')
     ON CONFLICT (tenant_id, subject, role_key) DO NOTHING`,
    [TENANT_ID, localId],
  );
  await pg.end();
  console.log(`Granted tenant_admin to ${EMAIL} in tenant ${TENANT_ID}`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
