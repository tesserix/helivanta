"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
  type ComponentProps,
} from "react";
import {
  useDisplayedAccountPresentations,
  useTenantTransition,
  type MemberPresentation,
  type StaffAssignmentScope,
} from "@helivanta/api";
import { SelectControl, type SelectOption } from "./select";
import { Controller, type Control, type FieldPath, type FieldValues } from "react-hook-form";
import { Field } from "./form";

type Presentations = {
  register: (subject: string) => () => void;
  rows: ReadonlyMap<string, MemberPresentation>;
  resolved: boolean;
};
const Context = createContext<Presentations | null>(null);

export function AccountPresentationsProvider({
  scope,
  children,
}: {
  scope: StaffAssignmentScope | null;
  children: ReactNode;
}) {
  const switching = useTenantTransition();
  const current = switching ? null : scope;
  return <Presentations scope={current}>{children}</Presentations>;
}

function Presentations({
  scope,
  children,
}: {
  scope: StaffAssignmentScope | null;
  children: ReactNode;
}) {
  const [registered, setRegistered] = useState<ReadonlyMap<string, number>>(() => new Map());
  const register = useCallback((subject: string) => {
    setRegistered((previous) => new Map(previous).set(subject, (previous.get(subject) ?? 0) + 1));
    return () =>
      setRegistered((previous) => {
        const next = new Map(previous);
        const count = (next.get(subject) ?? 1) - 1;
        if (count) next.set(subject, count);
        else next.delete(subject);
        return next;
      });
  }, []);
  const subjects = [...registered.keys()].sort();
  const query = useDisplayedAccountPresentations(scope, subjects);
  const rows = useMemo(
    () =>
      new Map(!scope || query.isError ? [] : (query.data?.map((row) => [row.subject, row]) ?? [])),
    [scope, query.isError, query.data],
  );
  return (
    <Context.Provider value={{ register, rows, resolved: query.isFetched }}>
      {children}
    </Context.Provider>
  );
}

export function useAccountPresentation(subject: string | null | undefined) {
  const rows = useAccountPresentations(subject ? [subject] : []);
  const row = subject ? rows.get(subject) : undefined;
  return row?.available ? row : undefined;
}

const noRows: ReadonlyMap<string, MemberPresentation> = new Map();

export function useAccountPresentations(subjects: readonly string[]) {
  const context = useContext(Context);
  const register = context?.register;
  const key = JSON.stringify([...new Set(subjects.filter(Boolean))].sort());
  const wanted = useMemo(() => JSON.parse(key) as string[], [key]);
  useEffect(() => {
    if (!register) return;
    const cleanups = wanted.map(register);
    return () => cleanups.forEach((cleanup) => cleanup());
  }, [wanted, register]);
  return context?.rows ?? noRows;
}

export function AccountSelectControl({
  subjects,
  emptyLabel = "Not assigned",
  ...props
}: Omit<ComponentProps<typeof SelectControl>, "options"> & {
  subjects: readonly string[];
  emptyLabel?: string;
}) {
  const people = useAccountPresentations(subjects);
  const options = [...new Set(subjects)].flatMap((subject) => {
    if (!subject) return [{ value: "", label: emptyLabel }];
    const person = people.get(subject);
    return person?.available
      ? [
          {
            value: subject,
            label: [person.name, person.roles.map((role) => role.label).join(", ")]
              .filter(Boolean)
              .join(" · "),
          },
        ]
      : [];
  });
  const context = useContext(Context);
  const selectedIsAvailable = options.some((option) => option.value === props.value);
  const { value, onValueChange, disabled } = props;
  useEffect(() => {
    if (context?.resolved && value && !disabled && !selectedIsAvailable) onValueChange("");
  }, [context?.resolved, value, disabled, selectedIsAvailable, onValueChange]);
  return (
    <SelectControl
      {...props}
      options={options}
      value={options.some((option) => option.value === props.value) ? props.value : ""}
    />
  );
}

export function AccountName({
  subject,
  fallback = "Account details unavailable",
  children,
}: {
  subject: string | null | undefined;
  fallback?: string;
  children?: (name: string | undefined) => ReactNode;
}) {
  const person = useAccountPresentation(subject);
  if (children) return children(subject ? (person?.name ?? fallback) : undefined);
  return <span>{person?.name ?? (subject ? fallback : "Not recorded")}</span>;
}

export function AccountRole({
  subject,
  roleKey,
}: {
  subject: string | null | undefined;
  roleKey: string;
}) {
  const person = useAccountPresentations(subject ? [subject] : []).get(subject ?? "");
  const role = person?.roles.find((role) => role.key === roleKey);
  return (
    <span>
      {role?.label ??
        (roleKey === "staff_member"
          ? "Staff member"
          : roleKey === "tenant_admin"
            ? "Tenant administrator"
            : "Organization role")}
    </span>
  );
}

export function FormAccountSelectField<T extends FieldValues>(props: {
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
      control={props.control}
      name={props.name}
      render={({ field, fieldState }) => (
        <Field id={props.id} label={props.label} error={fieldState.error?.message}>
          <AccountSelectControl
            id={props.id}
            name={field.name}
            ref={field.ref}
            onBlur={field.onBlur}
            subjects={props.options.map((option) => option.value)}
            emptyLabel={props.options.find((option) => option.value === "")?.label}
            value={field.value ?? ""}
            disabled={props.disabled}
            aria-invalid={fieldState.invalid}
            aria-describedby={fieldState.error ? `${props.id}-error` : undefined}
            onValueChange={(value) => {
              field.onChange(value === "" ? (props.emptyValue === null ? null : "") : value);
              props.onValueChange?.(value);
            }}
          />
        </Field>
      )}
    />
  );
}
