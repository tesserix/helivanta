"use client";
import { useState } from "react";
import { Button } from "@tesserix/web";
import {
  getSupplierEmployer,
  registerSupplierEmployer,
  useApiMutation,
  usePermissions,
  type SupplyRegistryScope,
  type SupplierEmployer,
} from "@helivanta/api";
import { GuardedLink, Panel } from "@helivanta/ui";
export function SupplierEmployerRegistration({ scope }: { scope: SupplyRegistryScope }) {
  const { can } = usePermissions();
  if (!can("workforce.supplier.manage")) return null;
  return <Registration key={`${scope.tenantId}:${scope.subject}:${scope.partyId}`} scope={scope} />;
}
function Registration({ scope }: { scope: SupplyRegistryScope }) {
  const [pending, setPending] = useState(false);
  const [result, setResult] = useState<SupplierEmployer | null>(null);
  const success = (value: SupplierEmployer) => {
    setResult(value);
    setPending(false);
  };
  const write = useApiMutation(() => registerSupplierEmployer(scope, scope.partyId), {
    onSuccess: success,
    suppressErrorToast: true,
  });
  const recover = useApiMutation(() => getSupplierEmployer(scope, scope.partyId), {
    onSuccess: success,
    suppressErrorToast: true,
  });
  return (
    <Panel className="space-y-3">
      <h4 className="font-semibold">Supplier Staff employer</h4>
      <p className="text-sm text-muted-foreground">
        Register this owned organisation to keep its Staff records and reviewed accounts. Location
        duties are assigned separately.
      </p>
      {result ? (
        <>
          <p role="status">Supplier Staff directory registration verified.</p>
          <p>{result.name}</p>
          <GuardedLink href="/supply/staff" className="text-sm underline">
            Manage supplier Staff and accounts
          </GuardedLink>
        </>
      ) : pending ? (
        <>
          <p role={write.isError || recover.isError ? "alert" : "status"}>
            {write.isPending
              ? "Registering supplier employer…"
              : "The registration outcome is unconfirmed. Recover it before continuing."}
          </p>
          {!write.isPending && (
            <Button variant="outline" disabled={recover.isPending} onClick={() => recover.mutate()}>
              Recover employer registration
            </Button>
          )}
        </>
      ) : (
        <Button
          onClick={() => {
            setPending(true);
            write.mutate();
          }}
        >
          Register supplier employer
        </Button>
      )}
    </Panel>
  );
}
