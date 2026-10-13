# ADR 0010: Hospital workspace and tenant branding

Status: implementation.

The first workforce UI uses the existing workforce API and adds a paginated hospital discovery read, limited to the caller's explicit hospital grants. Tenant administrators do not inherit staff access. Tenant switching remains a session operation; hospital selection only selects an already authorized hospital.

Tenant owners manage one published branding record through `workspace.branding.manage`. Its public projection contains only display name, welcome text, HTTPS logo URL, one of three medical color palettes and centered/split login layout. The authenticated read derives the tenant from the signed session. The anonymous read accepts a tenant UUID only to fetch deliberately public branding; it cannot supply authentication context or authorize data access. PostgreSQL RLS, strict input validation and immutable mutation audit protect writes. Arbitrary HTML, scripts, CSS and redirect URLs are not supported. Logos are browser-rendered images with no referrer; the server never fetches tenant URLs. File uploads remain a separate GCS/ClamAV pipeline and are not introduced by accepting an existing HTTPS image URL.

A tenant-specific login link retains a cosmetic tenant hint through OIDC redirects. After login, the verified session selects branding and data; the hint never selects membership. Owner settings and previews always include “Powered by Tesserix” and copyright. The existing password, MFA, session expiry and revocation controls remain authoritative.

Assets are staff identities, access grants and credentials. Attackers include anonymous callers, another tenant's member and malicious branding editors. Every API write checks a server-derived tenant and permission. React renders text, never user-authored markup; logo URLs reject credentials, non-HTTPS schemes and private hosts. No browser-local branding is an authoritative persisted setting.

Target: 100 tenants, up to 100 hospitals/tenant, 10,000 staff/hospital; 99.9% monthly availability and p95 read latency below 300 ms at 50 requests/s. Page size is bounded by existing keyset pagination (maximum 100). No additional deployment or datastore is required; incremental storage is below 100 MB at this scale plus audit retention. These are design targets, not measured guarantees.

PostgreSQL failure returns a bounded error; staff forms preserve unsaved input. OpenFGA failure denies access. Public branding failure renders the neutral Helivanta login without bypassing authentication. TanStack Query keys include hospital identity; switching hospitals unmounts the old directory so staff and draft forms cannot bleed across hospitals. No queue is required for these single-database writes.

Branding uses an additive forward migration with forced RLS. Roll back application images while retaining the table and audit history; do not drop production data. The simpler localStorage-only design is rejected because it cannot provide tenant-wide settings or trustworthy previews across devices. Freeform HTML/page builders are excluded because constrained templates meet this request with a smaller security surface.
