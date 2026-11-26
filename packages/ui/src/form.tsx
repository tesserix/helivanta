"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { useForm, type DefaultValues, type FieldValues } from "react-hook-form";
import { cloneElement, isValidElement, type ReactNode } from "react";
import type { z } from "zod";

// Helivanta forms are react-hook-form + zod with inline errors (spec D3).
// Always set noValidate on the <form> — native validation is banned.
export function useZodForm<S extends z.ZodType<FieldValues, FieldValues>>(
  schema: S & z.ZodType<z.output<S>, z.input<S>>,
  defaultValues?: DefaultValues<z.input<S>>,
) {
  return useForm<z.input<S>, unknown, z.output<S>>({
    resolver: zodResolver<z.input<S>, unknown, z.output<S>>(schema),
    defaultValues,
    mode: "onTouched",
  });
}

type ControlProps = {
  id?: string;
  "aria-invalid"?: boolean | "true" | "false" | "grammar" | "spelling";
  "aria-describedby"?: string;
};

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
  const errorId = `${id}-error`;
  // The control is the direct child carrying the Field's id; anything else
  // (a wrapper, no id) has no control to point at and is left alone.
  const control =
    error && isValidElement<ControlProps>(children) && children.props.id === id
      ? cloneElement(children, {
          // A control's own aria-invalid wins; its describedby is merged.
          "aria-invalid": children.props["aria-invalid"] ?? true,
          "aria-describedby": [
            ...new Set([...(children.props["aria-describedby"]?.split(/\s+/) ?? []), errorId]),
          ]
            .filter(Boolean)
            .join(" "),
        })
      : children;
  return (
    <div className="flex min-w-0 flex-col gap-1.5 text-sm font-medium">
      <label htmlFor={id}>{label}</label>
      {control}
      {error && (
        <p id={errorId} role="alert" className="text-sm font-normal text-destructive">
          {error}
        </p>
      )}
    </div>
  );
}
