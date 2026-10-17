"use client";

import { useContext, type ComponentProps } from "react";
import { BrandThemeContext } from "./brand-theme";
import { Controller, type Control, type FieldPath, type FieldValues } from "react-hook-form";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue, cn } from "@tesserix/web";
import { Field } from "./form";

export type SelectOption = { value: string; label: string; disabled?: boolean };
type SelectControlProps = Omit<ComponentProps<typeof SelectTrigger>, "value" | "onChange"> & {
  value?: string;
  onValueChange: (value: string) => void;
  options: readonly SelectOption[];
  placeholder?: string;
  name?: string;
};

const EMPTY_VALUE = "__helivanta_empty__";

export function SelectControl({
  value,
  onValueChange,
  options,
  placeholder = "Select an option",
  name,
  className,
  ...trigger
}: SelectControlProps) {
  const theme = useContext(BrandThemeContext);
  const encode = (choice: string) => (choice === "" ? EMPTY_VALUE : choice);
  const selected =
    value === "" && options.some((option) => option.value === "") ? EMPTY_VALUE : value;
  return (
    <Select
      name={name}
      value={selected}
      onValueChange={(choice) => {
        if (choice !== "") onValueChange(choice === EMPTY_VALUE ? "" : choice);
      }}
      disabled={trigger.disabled}
    >
      <SelectTrigger
        {...trigger}
        className={cn(
          "min-w-0 rounded-lg bg-background text-left shadow-none data-[size=default]:h-11 focus-visible:ring-2 focus-visible:ring-(--hms-accent)",
          className,
        )}
      >
        <SelectValue className="min-w-0 flex-1 truncate text-left" placeholder={placeholder} />
      </SelectTrigger>
      <SelectContent
        data-brand-theme={theme}
        position="popper"
        sideOffset={6}
        className="z-50 max-h-[min(20rem,var(--radix-select-content-available-height))] max-w-[calc(100vw-2rem)] rounded-xl border bg-popover text-popover-foreground shadow-xl motion-reduce:animate-none"
      >
        {options.map((option) => (
          <SelectItem
            key={option.value}
            value={encode(option.value)}
            disabled={option.disabled}
            className="min-h-10 cursor-pointer rounded-lg px-3 pr-9 text-sm focus:bg-(--hms-accent-dim) focus:text-(--hms-accent)"
          >
            <span className="break-words">{option.label}</span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

export function FormSelectField<T extends FieldValues>({
  control,
  name,
  id,
  label,
  options,
  disabled,
  emptyValue = "",
  onValueChange,
}: {
  control: Control<T>;
  name: FieldPath<T>;
  id: string;
  label: string;
  options: readonly SelectOption[];
  disabled?: boolean;
  emptyValue?: "" | null;
  onValueChange?: (value: string) => void;
}) {
  return (
    <Controller
      control={control}
      name={name}
      render={({ field, fieldState }) => (
        <Field id={id} label={label} error={fieldState.error?.message}>
          <SelectControl
            id={id}
            name={field.name}
            ref={field.ref}
            value={field.value ?? ""}
            onValueChange={(value) => {
              field.onChange(value === "" ? emptyValue : value);
              onValueChange?.(value);
            }}
            onBlur={field.onBlur}
            options={options}
            disabled={disabled}
            aria-invalid={fieldState.invalid}
            aria-describedby={fieldState.error ? `${id}-error` : undefined}
          />
        </Field>
      )}
    />
  );
}
