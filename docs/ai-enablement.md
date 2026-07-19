# AI Enablement — MVP 6 (M18+)

Status: Planned. Board: [project #12](https://github.com/orgs/tesserix/projects/12), phase **MVP 6 — AI Enablement (M18+)**, issues **#717–#753**, all labelled `area:ai`.

AI enters the platform *after* the foundation is earned: real clinical data flows, the SDK enforces tenancy/consent/audit, and telemetry exists. MVP 6 then adds AI as a governed platform capability — never as per-team experiments.

## Strategy in one paragraph

Build the **AI platform first** (agent framework, gateway, telemetry, guardrails, evals, registry, RAG, tools, HITL, governance, cost, synthetic data — #717–#728), then ship **assistive AI features per product** (#729–#753) on top of it. Every feature follows four non-negotiables: **assistive not autonomous** (a human sees, edits, approves), **grounded and cited** (no unverifiable claims in clinical contexts), **consent-, tenant- and country-scoped data** (RAG and prompts inherit the platform's isolation), and **measured** (golden-dataset evals gate every prompt/model change).

## Order of build

1. **ADK & agent runtime (#717)** — one framework (Google ADK primary candidate, fits the Vertex/Gemini/Gemma stack), one runtime pattern wired into the SDK (tenancy, authn/z, audit, telemetry). Decision recorded as an ADR.
2. **LLM gateway (#718) + AI telemetry (#719)** — all model traffic through one governed, metered, traceable door before any feature ships. OTel GenAI semantics; token/cost/latency per tenant and use-case; full agent traces.
3. **Safety rails (#720, #721, #722)** — guardrails (PHI-minimised prompts, injection defence, output filters), eval harness with clinician-approved golden datasets gating changes in CI, versioned prompt/agent registry with staged rollout and instant rollback.
4. **Grounding & action (#723, #724, #725)** — consent-aware RAG (retrieval enforces FGA + consent artefacts per chunk), FGA-scoped tool calling (agents act with the *user's* authority, audited), human-in-the-loop approval framework for consequential actions.
5. **Governance & economics (#726, #727, #728)** — clinical-safety/regulatory envelope (assistive positioning, DPDP consent for AI processing, kill-switch runbook), per-tenant budgets/quotas/showback, synthetic clinical data for PHI-free development.

## Where AI helps, module by module

| Product | AI capability | Issue |
|---|---|---|
| MediCore | Ambient clinical documentation (AI scribe) | #729 |
| MediCore | Discharge summary generation (clinician sign-off) | #730 |
| MediCore | ICD/diagnosis coding suggestions | #731 |
| MediCore | Bed occupancy & admission forecasting | #732 |
| MediConnect | Symptom triage assistant (assistive, not diagnostic) | #733 |
| MediConnect | Patient support chatbot on WhatsApp (KB-grounded) | #734 |
| MediConnect | Plain-language, multilingual record summaries | #735 |
| DoctorConnect | Chart summarization on patient open | #736 |
| DoctorConnect | e-Prescription drafting (rules-engine constrained) | #737 |
| DoctorConnect | Referral & medical letter drafting | #738 |
| LabConnect | Report narrative summaries (pathologist-approved) | #739 |
| LabConnect | QC anomaly detection on analyser data | #740 |
| PharmaConnect | Prescription validation assistant | #741 |
| PharmaConnect | Medicine demand forecasting | #742 |
| EmergencyConnect | Dispatch triage & priority scoring (assistive) | #743 |
| EmergencyConnect | Crew-note pre-alert summarization for the ED | #744 |
| CareConnect | Remote-vitals anomaly detection & alerts | #745 |
| CareConnect | Personalised adherence nudging | #746 |
| SupplyConnect | Demand forecasting & expiry-risk optimisation | #747 |
| FacilityConnect | Predictive maintenance from asset telemetry | #748 |
| AdminConnect | Natural-language analytics (de-identified warehouse only) | #749 |
| AdminConnect | Tenant churn & health prediction | #750 |
| Insurance (MediCore) | Claims documentation assistant & denial-risk prediction | #751 |
| Support (AdminConnect) | Support copilot & ticket auto-triage | #752 |
| SafetyConnect | Drill/incident report auto-summaries & findings extraction | #753 |

Existing document-AI work (Prescription OCR, lab-report OCR, discharge-summary *extraction*, document classification — MVP 4/5) is the precursor track; MVP 6 generalises it into the platform above.

## Hard rules carried by every AI feature

- Clinicians/users always see, can edit, and must approve consequential output; provenance ("AI-assisted") is recorded.
- Low-confidence output is flagged, never presented as definitive; red-flag/emergency paths bypass AI entirely.
- AI unavailability degrades to the manual workflow — it never blocks care.
- Prompts and retrieval are PHI-minimised, consent-checked, tenant- and country-scoped; no PHI leaves the country boundary.
- Diagnosis and treatment decisions are out of scope for autonomous AI everywhere in the platform.

## Suggested first wave (when MVP 6 opens)

Platform items #717–#721 in parallel with two flagship features: **chart summarization (#736)** — highest clinician value, lowest risk (read-only, cited) — and **WhatsApp support bot (#734)** — highest patient-visible value on non-clinical ground. The scribe (#729) follows once the eval harness and HITL framework are proven.
