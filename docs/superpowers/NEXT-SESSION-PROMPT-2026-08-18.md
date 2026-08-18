Take over Helivanta platform work. Repo: /Users/Mahesh.Sangawar/personal/tesserix-new/helivanta

Infrastructure manifests live in a second repo: /Users/Mahesh.Sangawar/personal/tesserix-new/tesserix-k8s (ADR-0001 — charts and ArgoCD apps are never in the app repo). Most work spans both.

Read docs/superpowers/HANDOFF-2026-08-18.md first — it is the current state and supersedes HANDOFF-2026-08-17-evening.md.

Then read docs/superpowers/specs/2026-08-17-deployment-artefacts-design.md (D1–D9, covers slices 1a and 1b) and docs/superpowers/plans/2026-08-18-deployment-slice-1b.md. Both carry inline corrections marked "Corrected <date>" — those corrections are the interesting part, because each one was a claim that read as authoritative and was false.

Where things stand: the platform dependencies are LIVE in production — CNPG Postgres, NATS with JetStream, OpenFGA, network policies, quota, OpenBao secrets — as seven Synced/Healthy ArgoCD Applications. The Helivanta application itself is at zero. No image has ever been published, because GitHub Actions fails instantly on a spending limit. A manual docker push was proposed and rejected: it would have no Trivy gate, no SBOM, no signature, and it is the artefact that would get promoted. Do not revisit that without asking.

Four blockers, all human-owned, in strict order: (1) Actions billing, (2) the two boot secrets via the secret-service console, (3) the Zitadel app and machine user on production Zitadel, (4) the Cloudflare tunnel ingress PUT and two DNS records. The handoff has exact commands for each. With (1) and (2) done, the API deploys by adding one line to argocd/prod/apps/helivanta/kustomization.yaml — the chart is drafted in tesserix-k8s#411 and deliberately not activated.

Follow the repo's process: search the 750+ existing issues before creating one, design before code (issue → spec → plan → implementation), and use subagent-driven execution with an independent review between tasks.

Four habits, each of which found real defects and should continue:

- Green means nothing until something has been mutated. For every control, ask what mutation would break it, apply it, and watch it fail. The boot guard was proven by deleting its call site from main.go and watching the process boot past; the OpenBao grant by a 403 on a path that exists; JetStream by querying account info rather than trusting a Ping.
- Verify against a live working object, never documentation. Every defect this session came from diffing against something already running — the policy template that mints the grant, a working ExternalSecret in another namespace, a live pod's container ports, the object the API server actually stored. Documentation was wrong three separate times.
- Ask what consumes a thing, and whether that has ever run. All three structural gaps — unreadable OpenBao paths, an orphaned cloudflared configmap, a missing migrate image — were errors in task briefs that review and CI both passed. They surfaced only when something downstream tried to use the artefact.
- When a subagent pushes back with evidence, it is usually right. Four did this session, each correctly.

Two things are inferred and must not be treated as established: TRUSTED_PROXY_CIDRS=10.20.0.0/16 (nobody has read the real X-Forwarded-For at an app pod — slice 1b Task 9 exists to confirm or correct it, and a forged header must be shown not to buy a fresh rate-limit bucket), and the Trivy gate plus Cosign signature, which merged without ever being watched working.

Environment traps that all present as "it is broken": leftover next dev processes holding ports; zitadel-login latching a stale PAT (docker restart helivanta-dev-zitadel-login-1); Zitadel core reporting unhealthy while OIDC discovery serves 200; ArgoCD serving CACHED manifest errors so an app-of-apps reports Synced while never having created a child (hard refresh, and always compare .status.sync.revision to origin/main); ct lint failing the whole job over a missing chart version bump or a 200-character line; BSD sed silently ignoring \b (use perl -pi -e).

Two standing rules with teeth: tesserix-k8s#392 (the scope-probe grant) must stay UNMERGED forever — merging it creates the policy and destroys the negative control proving the OpenBao grant is bounded. And Postgres runs instances: 1 with backups disabled, which is fine while the database is empty and must be raised before Helivanta holds a patient record.

main has everything merged through #876. Open: helivanta#874 (the slice 1b plan), tesserix-k8s#411 (the API chart, draft, do not activate).
