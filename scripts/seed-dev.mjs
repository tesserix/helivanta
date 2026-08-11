// Seeds the GIP emulator with two dev users and grants each a role via
// iam_members: test@hms.dev (tenant_admin) and pharmacist@hms.dev
// (pharmacist only). The second user is what makes permission gating
// observable by hand and testable in e2e — everything test@hms.dev can
// do, pharmacist@hms.dev mostly cannot.
// Usage: node scripts/seed-dev.mjs   (emulator + Postgres must be up and
// migrated — `make seed` runs `make migrate` first for exactly this reason)
const HOST = process.env.AUTH_EMULATOR_HOST ?? "localhost:9099";
const PROJECT = process.env.GIP_PROJECT_ID ?? "demo-hms";
const TENANT_ID = "11111111-1111-1111-1111-111111111111";
const PASSWORD = "password123";

const USERS = [
  { email: "test@hms.dev", role: "tenant_admin" },
  { email: "pharmacist@hms.dev", role: "pharmacist" },
];

const base = `http://${HOST}/identitytoolkit.googleapis.com/v1`;
const headers = {
  "Content-Type": "application/json",
  Authorization: "Bearer owner",
};

// Signs a user up in the GIP emulator (or resolves their existing id),
// sets the tenant_id custom claim, and returns the Firebase UID. That
// UID is what authn.Principal.Subject carries — it must match the
// `subject` written into iam_members below or the seeded user has no
// permissions.
async function seedAuthUser(email) {
  const signUp = await fetch(`${base}/accounts:signUp?key=demo-key`, {
    method: "POST",
    headers,
    body: JSON.stringify({
      email,
      password: PASSWORD,
      returnSecureToken: true,
    }),
  });
  const created = await signUp.json();
  if (!signUp.ok && created?.error?.message !== "EMAIL_EXISTS") {
    throw new Error(`signUp failed for ${email}: ${JSON.stringify(created)}`);
  }
  let localId = created.localId;
  if (!localId) {
    const lookup = await fetch(
      `http://${HOST}/emulator/v1/projects/${PROJECT}/accounts:query`,
      { method: "POST", headers, body: JSON.stringify({}) },
    ).then((r) => r.json());
    localId = lookup.userInfo?.find((u) => u.email === email)?.localId;
  }
  if (!localId) throw new Error(`could not resolve user id for ${email}`);

  const update = await fetch(`${base}/accounts:update?key=demo-key`, {
    method: "POST",
    headers,
    body: JSON.stringify({
      localId,
      customAttributes: JSON.stringify({ tenant_id: TENANT_ID }),
    }),
  });
  if (!update.ok) {
    throw new Error(`set claims failed for ${email}: ${await update.text()}`);
  }
  return localId;
}

async function main() {
  const { Client } = await import("pg");
  const pg = new Client({
    connectionString:
      process.env.ADMIN_DATABASE_URL ??
      "postgres://hms:hms@localhost:5432/hms?sslmode=disable",
  });
  await pg.connect();

  try {
    for (const { email, role } of USERS) {
      const localId = await seedAuthUser(email);
      console.log(`Seeded ${email} / ${PASSWORD} with tenant_id=${TENANT_ID}`);

      // Bootstrap: the first tenant_admin cannot be granted through a
      // route that requires iam.member.manage, so the seed writes the
      // membership row directly for every seeded user. The API's
      // reconciler turns it into tuples on next boot; the iam-fga-sync
      // consumer does it immediately for later grants.
      await pg.query(
        `INSERT INTO iam_members (tenant_id, subject, role_key)
         VALUES ($1, $2, $3)
         ON CONFLICT (tenant_id, subject, role_key) DO NOTHING`,
        [TENANT_ID, localId, role],
      );
      console.log(`Granted ${role} to ${email} in tenant ${TENANT_ID}`);
    }
  } finally {
    await pg.end();
  }

  console.log(`Seeded:
  test@hms.dev       / password123  (tenant_admin — sees every zone)
  pharmacist@hms.dev / password123  (pharmacist — sees Pharmacy only)`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
