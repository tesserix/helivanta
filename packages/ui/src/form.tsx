"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { useForm, type DefaultValues, type FieldValues } from "react-hook-form";
import type { ReactNode } from "react";
import type { z } from "zod";

// HMS forms are react-hook-form + zod with inline errors (spec D3).
// Always set noValidate on the <form> — native validation is banned.
export function useZodForm<S extends z.ZodType<FieldValues>>(
  schema: S,
  defaultValues?: DefaultValues<z.infer<S>>,
) {
  return useForm<z.infer<S>>({
    resolver: zodResolver(schema),
    defaultValues,
    mode: "onTouched",
  });
}

export function Field({
  id,
  label,
  error,
  children,
}: {
  id: string;
  label: string;
  error?: string;
  children: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1.5 text-sm font-medium">
      <label htmlFor={id}>{label}</label>
      {children}
      {error && (
        <p role="alert" className="text-sm font-normal text-destructive">
          {error}
        </p>
      )}
    </div>
  );
}
