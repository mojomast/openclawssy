import { FoundationPlaceholderPage } from "@/components/FoundationPlaceholderPage"

export function AgentMonitorPage() {
  return (
    <FoundationPlaceholderPage
      title="Agent Monitor"
      description="Live agent monitoring with run launch, polling, and status cards."
      emptyMessage="No monitor snapshot is shown in this foundation placeholder yet. The full React Agent Monitor migration is in progress."
      testID="agent-monitor-page"
    />
  )
}
