# Workspace precondition implementation

1. Observe the live ignored assertion and a failing real owner regression.
2. Add the opt-in shared principal extractor, then adopt it at all authenticated
   MediConnect and patientmaster request extractions without changing workload APIs.
3. Cover strict header forms, verified-scope preservation, every route family,
   no-side-effect rejection and Global API forwarding; keep no-header compatibility.
4. Update HTTP/domain contracts. Run affected race packages, architecture,
   contract parity, build/vet/lint and required regression CI.
5. Merge reviewed passing code, promote through Kargo, and repeat bounded real QA
   read/write-precondition checks. Do not close proxy/disclosure stories on this fix.
