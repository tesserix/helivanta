"use client";
import { useState } from "react";
import { Input } from "@tesserix/web";
import { Field } from "@helivanta/ui";
import { countryConfiguration } from "@helivanta/api";
function currencyDigits(currency: string) {
  const configured = countryConfiguration.countries.find(
    (profile) => profile.currency === currency,
  );
  if (configured) return configured.currency_minor_digits;
  return /^[A-Z]{3}$/.test(currency)
    ? (new Intl.NumberFormat("en", { style: "currency", currency }).resolvedOptions()
        .maximumFractionDigits ?? 2)
    : 2;
}
export function formatPharmacyMoney(minor: number | string, currency: string) {
  const digits = currencyDigits(currency);
  const value = BigInt(minor),
    scale = 10n ** BigInt(digits),
    magnitude = value < 0n ? -value : value;
  return `${currency} ${value < 0n ? "-" : ""}${new Intl.NumberFormat("en", { maximumFractionDigits: 0 }).format(magnitude / scale)}${digits ? "." + (magnitude % scale).toString().padStart(digits, "0") : ""}`;
}
export function CurrencyAmountField({
  id,
  label,
  currency,
  initialMinor,
  error,
  onChange,
}: {
  id: string;
  label: string;
  currency: string;
  initialMinor: number;
  error?: string;
  onChange: (minor: number) => void;
}) {
  const digits = currencyDigits(currency);
  const scale = 10n ** BigInt(digits);
  const minor = BigInt(Number.isSafeInteger(initialMinor) ? initialMinor : 0);
  const [value, setValue] = useState(() =>
    digits
      ? `${minor / scale}.${(minor % scale).toString().padStart(digits, "0")}`
      : minor.toString(),
  );
  function change(raw: string) {
    setValue(raw);
    const match = new RegExp(digits ? `^(\\d+)(?:\\.(\\d{0,${digits}}))?$` : "^(\\d+)$").exec(raw);
    if (!match) {
      onChange(Number.NaN);
      return;
    }
    const units = BigInt(match[1]) * scale + BigInt((match[2] ?? "").padEnd(digits, "0") || "0");
    onChange(units <= 1000000000000n ? Number(units) : Number.NaN);
  }
  return (
    <Field
      id={id}
      label={`${label} (${currency || "currency"})`}
      error={error ? "Enter a valid amount within the supported limit." : undefined}
    >
      <Input
        id={id}
        inputMode="decimal"
        maxLength={20}
        value={value}
        onChange={(event) => change(event.target.value)}
        aria-invalid={!!error}
        aria-describedby={error ? `${id}-error` : undefined}
      />
    </Field>
  );
}
