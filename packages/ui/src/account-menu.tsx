"use client";

import { useId, type FormEvent } from "react";
import { CircleUser, LogOut } from "lucide-react";
import { accountProfileSchema, useApiQuery } from "@helivanta/api";
import { Popover, PopoverContent, PopoverTrigger } from "./popover";

function initials(name: string) {
  const words = name.split(/\s+/).filter(Boolean);
  const first = (word: string | undefined) => (word ? (Array.from(word)[0] ?? "") : "");
  const last = words.length > 1 ? first(words.at(-1)) : "";
  return (first(words[0]) + last).toUpperCase();
}

// Who is signed in and as what (#1428 D3). Sign out never waits on the profile,
// so a shared terminal can always be left even when Zitadel is down.
export function AccountMenu({
  hospitalName,
  onSignOut,
}: {
  hospitalName: string;
  onSignOut: (e: FormEvent<HTMLFormElement>) => void | Promise<void>;
}) {
  const rolesHeading = useId();
  const query = useApiQuery(["iam", "me", "profile"], "/iam/me/profile");
  const parsed = accountProfileSchema.safeParse(query.data);
  const profile = parsed.success ? parsed.data : null;
  const name = profile ? profile.display_name.trim() || profile.email : "";

  return (
    <Popover>
      {/* Named by its content, not aria-label: see the label-in-name test. */}
      <PopoverTrigger className="flex items-center gap-2 rounded-full py-1 pl-1 pr-3 text-sm text-muted-foreground transition-colors hover:bg-accent hover:text-foreground">
        {name ? (
          <span
            aria-hidden="true"
            className="flex h-7 w-7 items-center justify-center rounded-full bg-primary text-xs font-semibold text-primary-foreground"
          >
            {initials(name)}
          </span>
        ) : (
          <CircleUser className="h-7 w-7" aria-hidden="true" />
        )}
        {name ? <span className="sr-only">Account:</span> : null}{" "}
        <span className="max-w-40 truncate">{name || "Account"}</span>
      </PopoverTrigger>
      <PopoverContent align="end" aria-label="Account" className="w-72 p-0">
        <div className="space-y-3 p-4">
          {profile ? (
            <>
              <div className="min-w-0">
                <p className="truncate font-medium text-foreground">{name}</p>
                {profile.email && profile.email !== name ? (
                  <p className="break-all text-sm text-muted-foreground">{profile.email}</p>
                ) : null}
              </div>
              <div>
                <p id={rolesHeading} className="text-xs font-medium text-muted-foreground">
                  Role at {hospitalName}
                </p>
                {profile.roles.length > 0 ? (
                  <ul aria-labelledby={rolesHeading} className="mt-1 space-y-0.5 text-sm">
                    {profile.roles.map((role) => (
                      <li key={role.key}>{role.label}</li>
                    ))}
                  </ul>
                ) : (
                  <p className="mt-1 text-sm">No role at this hospital</p>
                )}
              </div>
            </>
          ) : query.isPending ? (
            <p className="text-sm text-muted-foreground">Loading your profile…</p>
          ) : (
            <p className="text-sm text-muted-foreground">
              Your profile is unavailable right now. You can still sign out.
            </p>
          )}
        </div>
        <form onSubmit={onSignOut} className="border-t p-1">
          <button
            type="submit"
            className="flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          >
            <LogOut className="h-4 w-4" aria-hidden="true" />
            Sign out
          </button>
        </form>
      </PopoverContent>
    </Popover>
  );
}
