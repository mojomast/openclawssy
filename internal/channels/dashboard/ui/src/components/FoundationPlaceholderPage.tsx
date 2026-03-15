import { useEffect, useState } from "react"
import { PageShell } from "@/components/PageShell"

type FoundationPlaceholderPageProps = {
  title: string
  description: string
  emptyMessage: string
  testID?: string
}

const PLACEHOLDER_BOOTSTRAP_DELAY_MS = 250

export function FoundationPlaceholderPage({
  title,
  description,
  emptyMessage,
  testID,
}: FoundationPlaceholderPageProps) {
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const timer = window.setTimeout(() => {
      setLoading(false)
    }, PLACEHOLDER_BOOTSTRAP_DELAY_MS)

    return () => {
      window.clearTimeout(timer)
    }
  }, [])

  return (
    <div className="space-y-4 p-6" data-testid={testID}>
      <div className="space-y-2">
        <h2 className="text-2xl font-semibold tracking-tight">{title}</h2>
        <p className="text-sm text-muted-foreground">{description}</p>
      </div>

      <PageShell
        loading={loading}
        empty={!loading}
        emptyMessage={emptyMessage}
      >
        <></>
      </PageShell>
    </div>
  )
}
