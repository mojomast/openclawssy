import { FoundationPlaceholderPage } from "@/components/FoundationPlaceholderPage"

export function SandboxPage() {
  return (
    <FoundationPlaceholderPage
      title="Sandbox"
      description="Docker sandbox management with container lifecycle and image/volume operations."
      emptyMessage="No sandbox resources are shown in this foundation placeholder yet. The full React Sandbox migration is in progress."
      testID="sandbox-page"
    />
  )
}
