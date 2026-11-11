"use client";

import { useEffect, useRef } from "react";
import { Button } from "@tesserix/web";
import { X } from "lucide-react";
import {
  useTenantTransition,
  type EnrollmentRequestScope,
  type PharmacyStaff,
} from "@helivanta/api";
import { Panel } from "@helivanta/ui";
import { DocumentWorkspace } from "@/app/(workspace)/workforce/document-panel";

/** A pharmacy employer's enrollment, credential and qualification files (ADR-0054); no HR intake or classification. */
export function PharmacyStaffDocumentPanel({
  scope,
  pharmacy,
  staff,
  onContextChanged,
  onClose,
}: {
  scope: EnrollmentRequestScope;
  pharmacy: { pharmacy_id: string; name: string };
  staff: PharmacyStaff;
  onContextChanged: () => void;
  onClose: () => void;
}) {
  const switching = useTenantTransition();
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    heading.current?.scrollIntoView?.({ block: "start", behavior: "smooth" });
    heading.current?.focus();
  }, []);
  return (
    <Panel flush aria-labelledby="pharmacy-staff-documents-heading" className="mt-6 scroll-mt-24">
      <header className="flex items-start justify-between gap-4 border-b px-6 py-5">
        <div>
          <h3
            id="pharmacy-staff-documents-heading"
            ref={heading}
            tabIndex={-1}
            className="text-xl font-semibold outline-none"
          >
            Staff documents
          </h3>
          <p className="mt-1 text-sm text-muted-foreground">
            {staff.display_name} · {staff.employee_code} · {pharmacy.name}
          </p>
        </div>
        <Button variant="ghost" size="sm" onClick={onClose} aria-label="Close staff documents">
          <X className="size-4" />
        </Button>
      </header>
      {switching ? (
        <p role="status" className="p-6">
          Switching tenant. This document workspace has been cleared.
        </p>
      ) : (
        <DocumentWorkspace
          key={`${scope.tenantId}:${scope.subject}:${pharmacy.pharmacy_id}:${staff.id}`}
          scope={{
            tenantId: scope.tenantId,
            subject: scope.subject,
            pharmacyId: pharmacy.pharmacy_id,
            staffId: staff.id,
          }}
          onContextChanged={onContextChanged}
        />
      )}
    </Panel>
  );
}
