import { SourceSupplierOperations } from "@/components/suppliers/source-supplier-operations";
export default async function SourceSupplierOperationsPage({
  params,
}: {
  params: Promise<{ product: string; organization: string; location: string }>;
}) {
  const { product, organization, location } = await params;
  return (
    <SourceSupplierOperations
      product={product}
      organizationId={organization}
      locationId={location}
    />
  );
}
