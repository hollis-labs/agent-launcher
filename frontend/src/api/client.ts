import { createApiClient } from "@hollis-labs/sysop-ui"

// Same-origin: the Go binary serves both this SPA and the API, so an empty
// baseUrl resolves every request against the current origin.
const http = createApiClient({ baseUrl: "" })

export interface HealthInfo {
  status: string
}

/**
 * Concrete API client — one method per endpoint. The starter dashboard
 * only calls `getHealth`; add your application's endpoints here.
 */
export const apiClient = {
  getHealth: () => http.get<HealthInfo>("/api/health"),
}

export type AppApiClient = typeof apiClient
