import * as React from "react"
import { cn } from "@/lib/utils"

interface ResizablePanelProps {
  children: React.ReactNode
  defaultWidth: number
  minWidth?: number
  maxWidth?: number
  className?: string
  onResize?: (width: number) => void
  direction?: "left" | "right"
}

export function ResizablePanel({
  children,
  defaultWidth,
  minWidth = 150,
  maxWidth = 600,
  className,
  onResize,
  direction = "right",
}: ResizablePanelProps) {
  const [width, setWidth] = React.useState(defaultWidth)
  const [isResizing, setIsResizing] = React.useState(false)
  const panelRef = React.useRef<HTMLDivElement>(null)

  React.useEffect(() => {
    const handleMouseMove = (e: MouseEvent) => {
      if (!isResizing || !panelRef.current) return

      const rect = panelRef.current.getBoundingClientRect()
      let newWidth: number

      if (direction === "right") {
        newWidth = e.clientX - rect.left
      } else {
        newWidth = rect.right - e.clientX
      }

      const clampedWidth = Math.max(minWidth, Math.min(maxWidth, newWidth))
      setWidth(clampedWidth)
      onResize?.(clampedWidth)
    }

    const handleMouseUp = () => {
      setIsResizing(false)
    }

    if (isResizing) {
      document.addEventListener("mousemove", handleMouseMove)
      document.addEventListener("mouseup", handleMouseUp)
      document.body.style.cursor = "col-resize"
      document.body.style.userSelect = "none"
    }

    return () => {
      document.removeEventListener("mousemove", handleMouseMove)
      document.removeEventListener("mouseup", handleMouseUp)
      document.body.style.cursor = ""
      document.body.style.userSelect = ""
    }
  }, [isResizing, minWidth, maxWidth, direction, onResize])

  return (
    <div
      ref={panelRef}
      className={cn("relative flex flex-col", className)}
      style={{ width: `${width}px`, minWidth: `${minWidth}px`, maxWidth: `${maxWidth}px` }}
    >
      {children}
      <div
        className={cn(
          "absolute top-0 bottom-0 w-1 cursor-col-resize hover:bg-primary/50 active:bg-primary transition-colors",
          isResizing && "bg-primary",
          direction === "right" ? "right-0" : "left-0"
        )}
        onMouseDown={() => setIsResizing(true)}
      />
    </div>
  )
}

interface ResizableHandleProps {
  direction?: "horizontal" | "vertical"
  className?: string
  onResize?: (delta: number) => void
}

export function ResizableHandle({
  direction = "horizontal",
  className,
  onResize,
}: ResizableHandleProps) {
  const [isResizing, setIsResizing] = React.useState(false)
  const startPosRef = React.useRef({ x: 0, y: 0 })

  React.useEffect(() => {
    const handleMouseMove = (e: MouseEvent) => {
      if (!isResizing) return

      const deltaX = e.clientX - startPosRef.current.x
      const deltaY = e.clientY - startPosRef.current.y

      if (direction === "horizontal") {
        onResize?.(deltaX)
      } else {
        onResize?.(deltaY)
      }

      startPosRef.current = { x: e.clientX, y: e.clientY }
    }

    const handleMouseUp = () => {
      setIsResizing(false)
    }

    if (isResizing) {
      document.addEventListener("mousemove", handleMouseMove)
      document.addEventListener("mouseup", handleMouseUp)
    }

    return () => {
      document.removeEventListener("mousemove", handleMouseMove)
      document.removeEventListener("mouseup", handleMouseUp)
    }
  }, [isResizing, direction, onResize])

  const handleMouseDown = (e: React.MouseEvent) => {
    setIsResizing(true)
    startPosRef.current = { x: e.clientX, y: e.clientY }
  }

  return (
    <div
      className={cn(
        "shrink-0 transition-colors",
        direction === "horizontal"
          ? "w-1 cursor-col-resize hover:bg-border active:bg-primary"
          : "h-1 cursor-row-resize hover:bg-border active:bg-primary",
        isResizing && "bg-primary",
        className
      )}
      onMouseDown={handleMouseDown}
    />
  )
}
