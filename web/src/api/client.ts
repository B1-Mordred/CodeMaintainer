import createClient from "openapi-fetch";
import type { paths } from "./schema";

// This generated-type client prevents hand-maintained dashboard request and
// response models from drifting from the controller's canonical OpenAPI file.
const origin = typeof window === "undefined" ? "http://localhost" : window.location.origin;

export const api = createClient<paths>({
  baseUrl: `${origin}/api/v1`,
  fetch: (request) => globalThis.fetch(request),
});
