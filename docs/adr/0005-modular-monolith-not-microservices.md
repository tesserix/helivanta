# ADR-0005: Helivanta stays a modular monolith; services are extracted on a trigger, not by default

- **Status:** Accepted (2026-08-14). Relates to #674 (ADR process), #7, #45,
  #679, #8, #716.
- **Context:** Helivanta is one Go module producing one deployable
  (`backend/cmd/api`), with five modules — `iam`, `medicore`, `pharmacy`,
  `lab`, `reference` — registered in `bootstrap.Modules()` and mounted under a
  single `/v1` router. The frontend is the opposite shape: five Next.js zone
  apps, each with its own `basePath` and its own `/api/:path*` rewrite to that
  one API. So the system is already a vertical slice at the edge and a monolith
  at the core.

  The question raised was whether the backend should become microservices,
  matching the marketplace product's ~30-service topology.

  Three facts about the current state decide it:

  1. **The platform cannot deploy once yet.** #7 (GitOps deployment) and #45
     (secrets management) are open. A split multiplies an unsolved problem by
     the number of services.
  2. **There is no distributed tracing.** #679, #8 and #716 are open. #678 gave
     correlation IDs *within* a process; nothing sees across one. Introducing
     network partitions between modules while blind to what crosses them is the
     most expensive debugging position available.
  3. **There is one developer.** The primary benefit of microservices is
     independent deploy cadence for teams that would otherwise block each
     other. That benefit is currently zero; the costs are not.

  Two properties would also degrade immediately at every seam cut:

  - **Transactional publish.** `bus.Publish` writes to the outbox inside the
    same transaction as the business write, so an event cannot exist for a
    write that rolled back. Across a service boundary that guarantee ends.
  - **Tenant isolation.** RLS is one configuration with one boot-time lint
    (`LintRLS`) enumerating every table and one connection-pool role assertion.
    N services means N pools, N role configurations, and N places to get
    `FORCE ROW LEVEL SECURITY` wrong.

  Crucially, the usual "modular monolith now, split later" promise is normally
  unfunded, because nothing stops the boundaries eroding in the meantime. Here
  they are mechanically enforced: `depguard` denies cross-module imports,
  `TestModulesDoNotImportEachOther` fails CI on any that slip past, cross-module
  data flows only via events, every module owns its own migrations and declares
  its own permissions. The boundaries exist today without paying the network
  tax.

- **Decision:** Helivanta stays a modular monolith. Services are extracted
  individually when a specific trigger fires, never as a wholesale migration.

  **Triggers that justify extracting a component into its own service:**

  1. A materially different scaling profile — bursty, GPU-bound, or an order of
     magnitude more traffic than the rest. The AI & Document Intelligence epic
     (46 open issues) is the clearest candidate visible in the backlog today.
  2. A different compliance or data-residency boundary, such as a country
     profile requiring data to stay in-region.
  3. A runtime that is not Go.
  4. Independent teams with independent release cadence — that is, when there
     are actually teams.

  Cost, convenience, or a preference for the pattern are **not** triggers.

  **What the codebase keeps doing to keep extraction cheap:**

  - Contract packages (`internal/modules/<module>/contract`, #827) hold each
    event's subject constant and payload type, importable across modules while
    everything else stays forbidden. These are the published interfaces: if a
    module becomes a service, its contract package lifts out as a shared
    library rather than being untangled from a central registry.
  - Cross-module data flows only via events, through the outbox.
  - Each module owns its migrations, its permissions and its consumers.
  - No module reaches another's handlers, repositories or tables.

- **Consequences:**
  - The `[Go SDK]` issue series (#676–#695) continues to describe a topology
    Helivanta does not have; ADR-0004 already records that. This ADR extends the same
    reasoning to the runtime shape, not just the packaging.
  - A future extraction is a lift of one module plus its contract package,
    against enforced boundaries — not a discovery exercise.
  - When the first trigger fires, the work that ADR-0004 and this ADR defer
    becomes real: a service needs its own deployment, secrets, tracing and
    tenant-isolation configuration. That is a reason to close #7, #45 and #679
    before the trigger arrives, not after.
  - This decision should be revisited when any trigger above is met, and not
    otherwise. Relitigating it without a trigger is the failure mode this ADR
    exists to prevent.
