import * as React from "react"
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "./ui/card"
import { Button } from "./ui/button"
import { Loader2, AlertCircle, Inbox, Plus } from "lucide-react"
import { cn } from "@/lib/utils"

interface PageShellProps {
  children: React.ReactNode
  title?: string
  description?: string
  loading?: boolean
  error?: string | null
  empty?: boolean
  emptyMessage?: string
  emptyAction?: {
    label: string
    onClick: () => void
  }
  onRetry?: () => void
  className?: string
  contentClassName?: string
}

export function PageShell({
  children,
  title,
  description,
  loading = false,
  error = null,
  empty = false,
  emptyMessage = "No items found.",
  emptyAction,
  onRetry,
  className,
  contentClassName,
}: PageShellProps) {
  // Loading state
  if (loading) {
    return (
      <div className={cn("flex flex-col items-center justify-center min-h-[300px] gap-4", className)}>
        <Loader2 className="h-8 w-8 animate-spin text-primary" />
        <p className="text-sm text-muted-foreground">Loading...</p>
      </div>
    )
  }

  // Error state
  if (error) {
    return (
      <Card className={cn("border-destructive/50 bg-destructive/5", className)}>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-destructive">
            <AlertCircle className="h-5 w-5" />
            Error
          </CardTitle>
          {description && (
            <CardDescription className="text-destructive/80">
              {description}
            </CardDescription>
          )}
        </CardHeader>
        <CardContent>
          <p className="text-sm text-destructive mb-4">{error}</p>
          {onRetry && (
            <Button variant="outline" onClick={onRetry}>
              Try Again
            </Button>
          )}
        </CardContent>
      </Card>
    )
  }

  // Empty state
  if (empty) {
    return (
      <Card className={className}>
        <CardContent className={cn("flex flex-col items-center justify-center min-h-[300px] gap-4", contentClassName)}>
          <div className="rounded-full bg-muted p-4">
            <Inbox className="h-8 w-8 text-muted-foreground" />
          </div>
          <div className="text-center">
            <p className="text-sm font-medium">{emptyMessage}</p>
            {emptyAction && (
              <p className="text-xs text-muted-foreground mt-1">
                Get started by creating your first item
              </p>
            )}
          </div>
          {emptyAction && (
            <Button onClick={emptyAction.onClick}>
              <Plus className="h-4 w-4 mr-2" />
              {emptyAction.label}
            </Button>
          )}
        </CardContent>
      </Card>
    )
  }

  // Normal content with optional header
  if (title) {
    return (
      <Card className={className}>
        <CardHeader>
          <CardTitle>{title}</CardTitle>
          {description && <CardDescription>{description}</CardDescription>}
        </CardHeader>
        <CardContent className={contentClassName}>
          {children}
        </CardContent>
      </Card>
    )
  }

  // Plain content
  return (
    <div className={className}>
      {children}
    </div>
  )
}
