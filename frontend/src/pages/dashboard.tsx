import { EmptyState, SummaryCards } from "@hollis-labs/sysop-ui"
import { useEffect, useState } from "react"
import type { HealthInfo } from "../api/client"
import { useApi } from "../api/context"

/**
 * Starter page — polls the same-origin /api/health endpoint and shows the
 * result in the kit's SummaryCards strip. Replace this with real content;
 * see the @hollis-labs/sysop-ui README for the page-composition pattern.
 */
export function DashboardPage() {
  const api = useApi()
  const [health, setHealth] = useState<HealthInfo | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    api
      .getHealth()
      .then((info) => {
        if (!cancelled) setHealth(info)
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err))
      })
    return () => {
      cancelled = true
    }
  }, [api])

  if (error) {
    return <EmptyState variant="error" title="Could not reach the server" description={error} />
  }

  return (
    <SummaryCards
      cards={[
        { label: "Server", value: health ? health.status : "…" },
        { label: "UI", value: "ready" },
      ]}
    />
  )
}
