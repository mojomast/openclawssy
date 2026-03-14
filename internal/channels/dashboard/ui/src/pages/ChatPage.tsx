import { FoundationPlaceholderPage } from "@/components/FoundationPlaceholderPage"

export function ChatPage() {
  return (
    <FoundationPlaceholderPage
      title="Chat"
      description="Chat interface with streaming messages, tool timeline, and agent control."
      emptyMessage="No chat session is available in this foundation placeholder yet. The full React Chat migration is in progress."
      testID="chat-page"
    />
  )
}
