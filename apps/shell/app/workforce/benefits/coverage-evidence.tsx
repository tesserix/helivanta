"use client";
import { useEffect, useRef } from "react";
import { Button } from "@tesserix/web";
import { benefitEvidenceScope, type BenefitEnrollment, type BenefitScope } from "@helivanta/api";
import { EvidencePanel } from "../evidence-panel";
import { evidenceSource, PROPOSED_EVIDENCE } from "./benefits-ui";

// Evidence freezes at the decision, so only the waiting proposal takes new files.
export function CoverageEvidence({
  scope,
  enrollment,
  title,
  writable,
  onClose,
}: {
  scope: BenefitScope;
  enrollment: BenefitEnrollment;
  title: string;
  writable: boolean;
  onClose: () => void;
}) {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  return (
    <div role="region" aria-label={`${title} evidence`} className="border-b bg-muted/10 p-5">
      <div className="flex items-center justify-between gap-4">
        <h4 ref={heading} tabIndex={-1} className="font-semibold outline-none">
          {title}
        </h4>
        <Button variant="ghost" onClick={onClose}>
          Close
        </Button>
      </div>
      {enrollment.coverage && (
        <EvidencePanel
          source={evidenceSource}
          label="Evidence for the coverage in force"
          scope={benefitEvidenceScope(scope, enrollment.id, enrollment.coverage.proposal_id)}
          attachable={false}
        />
      )}
      {enrollment.pending && (
        <EvidencePanel
          source={evidenceSource}
          label={PROPOSED_EVIDENCE}
          scope={benefitEvidenceScope(scope, enrollment.id, enrollment.pending.id)}
          attachable={writable}
        />
      )}
    </div>
  );
}
