// Seeds the GIP emulator with two dev users and grants each a role via
// iam_members: test@hms.dev (tenant_admin) and pharmacist@hms.dev
// (pharmacist only). The second user is what makes permission gating
// observable by hand and testable in e2e — everything test@hms.dev can
// do, pharmacist@hms.dev mostly cannot.
//
// test@hms.dev additionally holds a *different* role (pharmacist) in a
// second hospital, which is what makes tenant switching testable: after
// switching, the same person must lose every zone tenant_admin gave them
// in the first hospital. A second membership with the same role would
// look identical before and after the switch and would pass even if the
// switch did nothing.
// Usage: node scripts/seed-dev.mjs   (emulator + Postgres must be up and
// migrated — `make seed` runs `make migrate` first for exactly this reason)
const HOST = process.env.AUTH_EMULATOR_HOST ?? "localhost:9099";
const TENANT_ID = "11111111-1111-1111-1111-111111111111";
// St Mary's, the second hospital test@hms.dev works at. The e2e tenant
// switch journey (e2e/tests/tenant-switch.spec.ts) hard-codes this id.
const SECOND_TENANT_ID = "22222222-2222-2222-2222-222222222222";
const PASSWORD = "password123";

// Each user's memberships, in the tenant their GIP claim starts them in
// first — seedAuthUser sets tenant_id from memberships[0].
const USERS = [
  {
    email: "test@hms.dev",
    memberships: [
      { tenantId: TENANT_ID, role: "tenant_admin" },
      { tenantId: SECOND_TENANT_ID, role: "pharmacist" },
    ],
  },
  {
    email: "pharmacist@hms.dev",
    memberships: [{ tenantId: TENANT_ID, role: "pharmacist" }],
  },
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
async function seedAuthUser(email, tenantId) {
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
    // Re-seeding an emulator that already has these users: sign in to
    // recover the uid. The emulator-only accounts:query endpoint used to
    // be the fallback here, but it 404s on current firebase-tools, which
    // made every re-seed fail once the users existed.
    const signIn = await fetch(
      `${base}/accounts:signInWithPassword?key=demo-key`,
      {
        method: "POST",
        headers,
        body: JSON.stringify({
          email,
          password: PASSWORD,
          returnSecureToken: true,
        }),
      },
    );
    const existing = await signIn.json();
    if (!signIn.ok) {
      throw new Error(
        `signIn failed for ${email}: ${JSON.stringify(existing)}`,
      );
    }
    localId = existing.localId;
  }
  if (!localId) throw new Error(`could not resolve user id for ${email}`);

  const update = await fetch(`${base}/accounts:update?key=demo-key`, {
    method: "POST",
    headers,
    body: JSON.stringify({
      localId,
      customAttributes: JSON.stringify({ tenant_id: tenantId }),
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
    for (const { email, memberships } of USERS) {
      const home = memberships[0];
      const localId = await seedAuthUser(email, home.tenantId);
      console.log(
        `Seeded ${email} / ${PASSWORD} with tenant_id=${home.tenantId}`,
      );

      // Bootstrap: the first tenant_admin cannot be granted through a
      // route that requires iam.member.manage, so the seed writes the
      // membership row directly for every seeded user. The API's
      // reconciler turns it into tuples on next boot; the iam-fga-sync
      // consumer does it immediately for later grants.
      for (const { tenantId, role } of memberships) {
        await pg.query(
          `INSERT INTO iam_members (tenant_id, subject, role_key)
           VALUES ($1, $2, $3)
           ON CONFLICT (tenant_id, subject, role_key) DO NOTHING`,
          [tenantId, localId, role],
        );
        console.log(`Granted ${role} to ${email} in tenant ${tenantId}`);
      }
    }
  } finally {
    await pg.end();
  }

  console.log(`Seeded:
  test@hms.dev       / password123  (tenant_admin in ${TENANT_ID} — sees every zone;
                                    pharmacist in ${SECOND_TENANT_ID} — switch to see Pharmacy only)
  pharmacist@hms.dev / password123  (pharmacist — sees Pharmacy only)`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
