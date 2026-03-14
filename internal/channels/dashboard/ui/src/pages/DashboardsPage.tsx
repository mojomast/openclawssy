import { FoundationPlaceholderPage } from "@/components/FoundationPlaceholderPage"

export function DashboardsPage() {
  return (
    <FoundationPlaceholderPage
      title="Custom Dashboards"
      description="Custom widget-based dashboards with drag/resize and persistence."
      emptyMessage="No custom dashboards are rendered in this foundation placeholder yet. The full React Dashboards migration is in progress."
      testID="dashboards-page"
    />
  )
}
