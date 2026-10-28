import { z } from "zod";
import { apiFetch } from "./client";
import {
  loginLinkContract,
  loginLinkHistoryContract,
  loginLinkInputContract,
  loginLinkDecisionInputContract,
  type LoginLinkInput,
  type LoginLinkDecisionInput,
} from "./login-link-contract";
export * from "./login-link-contract";
export { v7 as newLoginLinkId } from "uuid";

export type LoginLinkScope = { tenantId: string; hospitalId: string; staffId: string };
type PageOptions = { cursor?: string; signal?: AbortSignal };
const identifier = loginLinkContract.shape.id;
const commandID = identifier.refine((value) => value[14] === "7", "Use a new command ID.");
const pageContract = z
  .object({ next_cursor: z.string().nullable(), has_more: z.boolean() })
  .strict();
const linksPage = z.object({ data: z.array(loginLinkContract), page: pageContract }).strict();
const historyPage = z
  .object({ data: z.array(loginLinkHistoryContract), page: pageContract })
  .strict();

export const loginLinksKey = (scope: LoginLinkScope) => [
  "workforce",
  "login-links",
  scope.tenantId,
  scope.hospitalId,
  scope.staffId,
];

export const loginLinksPath = (scope: LoginLinkScope) =>
  `/workforce/hospitals/${identifier.parse(scope.hospitalId)}/staff/${identifier.parse(scope.staffId)}/login-links`;

function requestOptions(scope: LoginLinkScope): RequestInit {
  return {
    cache: "no-store",
    headers: { "X-Helivanta-Expected-Tenant": identifier.parse(scope.tenantId) },
  };
}

function owned(scope: LoginLinkScope, id: string, response: unknown) {
  const link = loginLinkContract.parse(response);
  if (link.id !== id || link.staff_id !== scope.staffId)
    throw new Error("The account link did not match this staff member.");
  return link;
}

export async function proposeLoginLink(scope: LoginLinkScope, id: string, input: LoginLinkInput) {
  const body = loginLinkInputContract.parse(input);
  return owned(
    scope,
    id,
    await apiFetch(`${loginLinksPath(scope)}/${commandID.parse(id)}`, {
      ...requestOptions(scope),
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
}

export async function decideLoginLink(
  scope: LoginLinkScope,
  id: string,
  input: LoginLinkDecisionInput,
) {
  const body = loginLinkDecisionInputContract.parse(input);
  return owned(
    scope,
    id,
    await apiFetch(`${loginLinksPath(scope)}/${commandID.parse(id)}/decision`, {
      ...requestOptions(scope),
      method: "POST",
      body: JSON.stringify(body),
    }),
  );
}

function pageQuery(options: PageOptions) {
  const params = new URLSearchParams({ limit: "50" });
  if (options.cursor) params.set("cursor", options.cursor);
  return params.toString();
}

export async function fetchLoginLinks(scope: LoginLinkScope, options: PageOptions = {}) {
  const page = linksPage.parse(
    await apiFetch(`${loginLinksPath(scope)}?${pageQuery(options)}`, {
      ...requestOptions(scope),
      signal: options.signal,
    }),
  );
  for (const link of page.data) owned(scope, link.id, link);
  return page;
}

export async function fetchLoginLinkHistory(
  scope: LoginLinkScope,
  id: string,
  options: PageOptions = {},
) {
  const page = historyPage.parse(
    await apiFetch(
      `${loginLinksPath(scope)}/${commandID.parse(id)}/history?${pageQuery(options)}`,
      {
        ...requestOptions(scope),
        signal: options.signal,
      },
    ),
  );
  if (page.data.some((entry) => entry.id !== id))
    throw new Error("The account link history did not match this link.");
  return page;
}
