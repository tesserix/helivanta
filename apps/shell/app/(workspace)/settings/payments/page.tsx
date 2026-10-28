"use client";

import { usePermissions } from "@helivanta/api";
import { PageHeading } from "@helivanta/ui";
import { RazorpayAccount } from "@/components/payments/razorpay-account";

export default function PaymentsSettingsPage() {
  const { can, isLoading } = usePermissions();
  return (
    <>
      <PageHeading description="The Razorpay account that verified UPI payments are collected into." />
      {isLoading ? (
        <p role="status">Loading…</p>
      ) : !can("payments.account.manage") ? (
        <p role="alert">Only tenant administrators can set the payment account.</p>
      ) : (
        <RazorpayAccount />
      )}
    </>
  );
}
