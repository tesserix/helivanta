import { HospitalSupplierOperations } from "@/components/suppliers/hospital-supplier-operations";
export default async function HospitalSupplierOperationsPage({
  params,
}: {
  params: Promise<{ hospital: string; department: string; pharmacy: string; outlet: string }>;
}) {
  const { hospital, department, pharmacy, outlet } = await params;
  return (
    <HospitalSupplierOperations
      hospitalId={hospital}
      departmentId={department}
      pharmacyId={pharmacy}
      outletId={outlet}
    />
  );
}
