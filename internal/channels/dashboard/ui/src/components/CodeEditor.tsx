import * as React from "react"
import { cn } from "@/lib/utils"
import { Button } from "./ui/button"
import { Copy, Check } from "lucide-react"

interface CodeEditorProps {
  value: string
  onChange?: (value: string) => void
  language?: string
  placeholder?: string
  readOnly?: boolean
  showLineNumbers?: boolean
  maxHeight?: string
  minHeight?: string
  className?: string
  onCopy?: () => void
  textareaTestId?: string
}

export function CodeEditor({
  value,
  onChange,
  language,
  placeholder,
  readOnly = false,
  showLineNumbers = true,
  maxHeight = "400px",
  minHeight = "100px",
  className,
  onCopy,
  textareaTestId,
}: CodeEditorProps) {
  const [copied, setCopied] = React.useState(false)
  const textareaRef = React.useRef<HTMLTextAreaElement>(null)
  const lineNumbersRef = React.useRef<HTMLDivElement>(null)

  const lines = value.split("\n")
  const lineCount = Math.max(1, lines.length)

  // Sync scroll between textarea and line numbers
  const handleScroll = (e: React.UIEvent<HTMLTextAreaElement>) => {
    if (lineNumbersRef.current) {
      lineNumbersRef.current.scrollTop = e.currentTarget.scrollTop
    }
  }

  const handleCopy = () => {
    navigator.clipboard.writeText(value)
    setCopied(true)
    onCopy?.()
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className={cn("relative rounded-md border bg-muted/50 flex flex-col", className)}>
      {/* Header */}
      <div className="flex items-center justify-between px-3 py-2 border-b bg-muted/80 shrink-0">
        <div className="flex items-center gap-2">
          {language && (
            <span className="text-xs font-medium text-muted-foreground px-2 py-0.5 bg-muted rounded">
              {language}
            </span>
          )}
          <span className="text-xs text-muted-foreground">
            {lineCount} line{lineCount !== 1 ? "s" : ""}
          </span>
        </div>
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

      {/* Editor */}
      <div className="flex overflow-auto" style={{ maxHeight, minHeight }}>
        {showLineNumbers && (
          <div
            ref={lineNumbersRef}
            className="select-none bg-muted/30 text-muted-foreground text-xs font-mono py-2 px-2 text-right shrink-0 border-r"
            style={{ minWidth: "2.5rem" }}
          >
            {Array.from({ length: lineCount }, (_, i) => (
              <div key={i} className="leading-5">
                {i + 1}
              </div>
            ))}
          </div>
        )}
        <textarea
          ref={textareaRef}
          data-testid={textareaTestId}
          value={value}
          onChange={(e) => onChange?.(e.target.value)}
          onScroll={handleScroll}
          placeholder={placeholder}
          readOnly={readOnly}
          spellCheck={false}
          className={cn(
            "flex-1 bg-transparent p-2 text-xs font-mono resize-none outline-none",
            "leading-5 whitespace-pre",
            readOnly && "cursor-text"
          )}
          style={{ tabSize: 2 }}
        />
      </div>
    </div>
  )
}

// Read-only code display with syntax highlighting
interface CodeBlockProps {
  code: string
  language?: string
  filename?: string
  showCopy?: boolean
  className?: string
  maxHeight?: string
}

export function CodeBlock({
  code,
  language,
  filename,
  showCopy = true,
  className,
  maxHeight = "300px",
}: CodeBlockProps) {
  const [copied, setCopied] = React.useState(false)
  const lines = code.split("\n")

  const handleCopy = () => {
    navigator.clipboard.writeText(code)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className={cn("rounded-md border bg-muted/50 overflow-hidden", className)}>
      {(filename || language || showCopy) && (
        <div className="flex items-center justify-between px-3 py-2 border-b bg-muted/80">
          <div className="flex items-center gap-2 min-w-0">
            {filename && (
              <span className="text-xs font-medium truncate">{filename}</span>
            )}
            {language && !filename && (
              <span className="text-xs font-medium text-muted-foreground px-2 py-0.5 bg-muted rounded">
                {language}
              </span>
            )}
          </div>
          {showCopy && (
            <Button
              variant="ghost"
              size="sm"
              className="h-7 px-2 shrink-0"
              onClick={handleCopy}
            >
              {copied ? (
                <Check className="h-3.5 w-3.5 text-green-500" />
              ) : (
                <Copy className="h-3.5 w-3.5" />
              )}
              <span className="ml-1 text-xs">{copied ? "Copied" : "Copy"}</span>
            </Button>
          )}
        </div>
      )}
      <div className="overflow-auto" style={{ maxHeight }}>
        <pre className="p-3 text-xs font-mono leading-5">
          <code>
            {lines.map((line, i) => (
              <div key={i} className="table-row">
                <span className="table-cell select-none text-muted-foreground text-right pr-3 w-8">
                  {i + 1}
                </span>
                <span className="table-cell whitespace-pre">{line || " "}</span>
              </div>
            ))}
          </code>
        </pre>
      </div>
    </div>
  )
}
