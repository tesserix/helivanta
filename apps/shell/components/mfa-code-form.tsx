"use client";

import { useEffect, useId, useRef } from "react";
import { AuthError, AuthSubmitButton, OTPInput } from "@tesserix/web";

type Props = {
  value: string;
  onValueChange: (value: string) => void;
  onSubmit: (code: string) => void;
  loading?: boolean;
  error?: string;
  label?: string;
  submitLabel?: string;
};

export function MfaCodeForm({
  value,
  onValueChange,
  onSubmit,
  loading = false,
  error,
  label = "Verification code",
  submitLabel = "Verify",
}: Props) {
  const id = useId();
  const group = useRef<HTMLDivElement>(null);
  const submitted = useRef<string | null>(null);
  const complete = /^\d{6}$/.test(value);

  useEffect(() => {
    group.current?.querySelector("input")?.focus();
  }, []);

  useEffect(() => {
    if (!complete) {
      submitted.current = null;
      return;
    }
    if (!loading && submitted.current !== value) {
      submitted.current = value;
      onSubmit(value);
    }
  }, [value, complete, loading, onSubmit]);

  return (
    <form
      noValidate
      className="flex flex-col gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        if (complete && !loading && submitted.current !== value) {
          submitted.current = value;
          onSubmit(value);
        }
      }}
    >
      <AuthError id={`${id}-error`} message={error} />
      <div className="space-y-3 text-center">
        <p id={`${id}-label`} className="text-sm font-medium">
          {label}
        </p>
        <OTPInput
          ref={group}
          length={6}
          value={value}
          onValueChange={onValueChange}
          disabled={loading}
          aria-label={undefined}
          aria-labelledby={`${id}-label`}
          aria-describedby={`${id}-help${error ? ` ${id}-error` : ""}`}
          aria-invalid={Boolean(error)}
          className="justify-center"
        />
        <p id={`${id}-help`} className="text-xs text-muted-foreground">
          Enter each digit, or paste the full six-digit code.
        </p>
      </div>
      <AuthSubmitButton loading={loading} disabled={!complete}>
        {submitLabel}
      </AuthSubmitButton>
    </form>
  );
}
