import createClient from "openapi-fetch";
import type { paths } from "./schema";

// This generated-type client prevents hand-maintained dashboard request and
// response models from drifting from the controller's canonical OpenAPI file.
const origin = typeof window === "undefined" ? "http://localhost" : window.location.origin;

let csrfToken = "";

export function setCSRFToken(token: string): void {
  csrfToken = token;
}

export function getCSRFToken(): string {
  return csrfToken;
}

export const api = createClient<paths>({
  baseUrl: `${origin}/api/v1`,
  fetch: (request) => {
    const headers = new Headers(request.headers);
    if (csrfToken && !["GET", "HEAD", "OPTIONS"].includes(request.method.toUpperCase())) {
      headers.set("X-CSRF-Token", csrfToken);
    }
    return globalThis.fetch(new Request(request, { credentials: "same-origin", headers }));
  },
});
