import { apiFetch } from "./client";
import {
  MemberGrantInputContract,
  MemberGrantResultContract,
  ProposalInputContract,
} from "./staff-access-contract";

export const staffMembershipSubjectContract = ProposalInputContract.shape.subject;

export async function enrollStaffMember(tenantId: string, subject: string) {
  const body = MemberGrantInputContract.parse({
    subject: staffMembershipSubjectContract.parse(subject),
    role_key: "staff_member",
  });
  return MemberGrantResultContract.parse(
    await apiFetch("/iam/members", {
      method: "POST",
      cache: "no-store",
      headers: {
        "X-Helivanta-Expected-Tenant": MemberGrantResultContract.shape.id.parse(tenantId),
      },
      body: JSON.stringify(body),
    }),
  );
}
