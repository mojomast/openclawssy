import * as React from "react"
import { cn } from "@/lib/utils"
import { Button } from "./ui/button"
import { Copy, Check } from "lucide-react"

interface JSONViewerProps {
  data: unknown
  className?: string
  maxHeight?: string
  expanded?: boolean
  searchable?: boolean
  onCopy?: () => void
}

export function JSONViewer({
  data,
  className,
  maxHeight = "400px",
  expanded = true,
  searchable = false,
  onCopy,
}: JSONViewerProps) {
  const [copied, setCopied] = React.useState(false)
  const [searchTerm, setSearchTerm] = React.useState("")

  const jsonString = React.useMemo(() => {
    try {
      return JSON.stringify(data, null, expanded ? 2 : undefined)
    } catch {
      return "Invalid JSON"
    }
  }, [data, expanded])

  const handleCopy = () => {
    navigator.clipboard.writeText(jsonString)
    setCopied(true)
    onCopy?.()
    setTimeout(() => setCopied(false), 2000)
  }

  const highlightedJson = React.useMemo(() => {
    if (!searchTerm) return jsonString
    const escaped = searchTerm.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")
    const regex = new RegExp(`(${escaped})`, "gi")
    return jsonString.replace(
      regex,
      '<mark class="bg-yellow-200 dark:bg-yellow-800">$1</mark>'
    )
  }, [jsonString, searchTerm])

  return (
    <div className={cn("relative rounded-md border bg-muted/50", className)}>
      <div className="flex items-center justify-between px-3 py-2 border-b bg-muted/80">
        <span className="text-xs font-medium text-muted-foreground">JSON</span>
        <Button
          variant="ghost"
          size="sm"
          className="h-7 px-2"
          onClick={handleCopy}
        >
          {copied ? (
            <Check className="h-3.5 w-3.5 text-green-500" />
          ) : (
            <Copy className="h-3.5 w-3.5" />
          )}
          <span className="ml-1 text-xs">{copied ? "Copied" : "Copy"}</span>
        </Button>
      </div>
      {searchable && (
        <div className="px-3 py-2 border-b">
          <input
            type="text"
            placeholder="Search in JSON..."
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
            className="w-full text-sm bg-transparent border-none outline-none placeholder:text-muted-foreground"
          />
        </div>
      )}
      <pre
        className="p-3 text-xs font-mono overflow-auto"
        style={{ maxHeight }}
        dangerouslySetInnerHTML={{ __html: highlightedJson }}
      />
    </div>
  )
}

// Simple tree view for nested objects
interface JSONTreeProps {
  data: unknown
  className?: string
}

export function JSONTree({ data, className }: JSONTreeProps) {
  const renderValue = (value: unknown, key: string, depth: number = 0): React.ReactNode => {
    const indent = "  ".repeat(depth)

    if (value === null) {
      return <span className="text-muted-foreground">null</span>
    }

    if (typeof value === "boolean") {
      return <span className="text-primary">{value.toString()}</span>
    }

    if (typeof value === "number") {
      return <span className="text-blue-600 dark:text-blue-400">{value}</span>
    }

    if (typeof value === "string") {
      return <span className="text-green-600 dark:text-green-400">"{value}"</span>
    }

    if (Array.isArray(value)) {
      if (value.length === 0) {
        return <span className="text-muted-foreground">[]</span>
      }
      return (
        <span>
          <span className="text-muted-foreground">[</span>
          <div className="pl-4">
            {value.map((item, index) => (
              <div key={index}>
                {renderValue(item, `${key}[${index}]`, depth + 1)}
                {index < value.length - 1 && <span className="text-muted-foreground">,</span>}
              </div>
            ))}
          </div>
          <span className="text-muted-foreground">{indent}]</span>
        </span>
      )
    }

    if (typeof value === "object") {
      const entries = Object.entries(value as Record<string, unknown>)
      if (entries.length === 0) {
        return <span className="text-muted-foreground">{"{}"}</span>
      }
      return (
        <span>
          <span className="text-muted-foreground">{"{"}</span>
          <div className="pl-4">
            {entries.map(([k, v], index) => (
              <div key={k}>
                <span className="text-foreground">{k}</span>
                <span className="text-muted-foreground">: </span>
                {renderValue(v, k, depth + 1)}
                {index < entries.length - 1 && <span className="text-muted-foreground">,</span>}
              </div>
            ))}
          </div>
          <span className="text-muted-foreground">{indent}{"}"}</span>
        </span>
      )
    }

    return <span>{String(value)}</span>
  }

  return (
    <div className={cn("font-mono text-xs overflow-auto", className)}>
      {renderValue(data, "root")}
    </div>
  )
}
