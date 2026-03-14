import * as React from "react"
import { cn } from "@/lib/utils"
import { Button } from "./ui/button"
import { Copy, Check } from "lucide-react"

type MarkdownLineKind = "plain" | "heading" | "list" | "quote" | "rule" | "code_fence"

const INLINE_MARKDOWN_PATTERN = /(`[^`\n]*`|\*\*[^*\n]+\*\*|~~[^~\n]+~~|\*[^*\n]+\*|\[[^\]\n]+\]\([^)]+\))/g

function supportsSyntaxHighlight(language?: string): boolean {
  const normalized = String(language || "").trim().toLowerCase()
  return normalized === "markdown" || normalized === "md" || normalized === "text" || normalized === "txt" || normalized === "plaintext"
}

function detectMarkdownLineKind(line: string): MarkdownLineKind {
  if (/^\s*```/.test(line)) {
    return "code_fence"
  }
  if (/^#{1,6}\s+/.test(line)) {
    return "heading"
  }
  if (/^\s*(?:[-*+]\s+|\d+\.\s+)/.test(line)) {
    return "list"
  }
  if (/^\s*>\s?/.test(line)) {
    return "quote"
  }
  if (/^\s*(?:-{3,}|\*{3,}|_{3,})\s*$/.test(line)) {
    return "rule"
  }
  return "plain"
}

function lineToneClass(kind: MarkdownLineKind): string {
  switch (kind) {
    case "heading":
      return "text-sky-700 dark:text-sky-300 font-semibold"
    case "list":
      return "text-emerald-700 dark:text-emerald-300"
    case "quote":
      return "text-amber-700 dark:text-amber-300"
    case "rule":
      return "text-muted-foreground"
    case "code_fence":
      return "text-violet-700 dark:text-violet-300"
    default:
      return ""
  }
}

function renderInlineMarkdown(content: string, keyPrefix: string): React.ReactNode[] {
  return content.split(INLINE_MARKDOWN_PATTERN).map((part, index) => {
    if (!part) {
      return null
    }

    const key = `${keyPrefix}-${index}`
    if (/^`[^`\n]*`$/.test(part)) {
      return <span key={key} className="text-violet-700 dark:text-violet-300">{part}</span>
    }

    if (/^\*\*[^*\n]+\*\*$/.test(part)) {
      return <span key={key} className="font-semibold text-foreground">{part}</span>
    }

    if (/^\*[^*\n]+\*$/.test(part)) {
      return <span key={key} className="italic text-foreground">{part}</span>
    }

    if (/^~~[^~\n]+~~$/.test(part)) {
      return <span key={key} className="line-through text-muted-foreground">{part}</span>
    }

    const linkMatch = part.match(/^\[([^\]]+)\]\(([^)]+)\)$/)
    if (linkMatch) {
      return (
        <span key={key}>
          <span className="text-sky-700 dark:text-sky-300">[{linkMatch[1]}]</span>
          <span className="text-cyan-700 dark:text-cyan-300">({linkMatch[2]})</span>
        </span>
      )
    }

    return <span key={key}>{part}</span>
  })
}

function renderMarkdownLine(line: string, lineIndex: number): React.ReactNode {
  if (!line) {
    return " "
  }

  const headingMatch = line.match(/^(#{1,6}\s+)(.*)$/)
  if (headingMatch) {
    return (
      <>
        <span className="text-sky-700/70 dark:text-sky-300/80">{headingMatch[1]}</span>
        {renderInlineMarkdown(headingMatch[2], `line-${lineIndex}-heading`)}
      </>
    )
  }

  const listMatch = line.match(/^(\s*(?:[-*+]\s+|\d+\.\s+))(.*)$/)
  if (listMatch) {
    return (
      <>
        <span className="text-emerald-700/70 dark:text-emerald-300/80">{listMatch[1]}</span>
        {renderInlineMarkdown(listMatch[2], `line-${lineIndex}-list`)}
      </>
    )
  }

  const quoteMatch = line.match(/^(\s*>\s?)(.*)$/)
  if (quoteMatch) {
    return (
      <>
        <span className="text-amber-700/80 dark:text-amber-300/80">{quoteMatch[1]}</span>
        {renderInlineMarkdown(quoteMatch[2], `line-${lineIndex}-quote`)}
      </>
    )
  }

  return renderInlineMarkdown(line, `line-${lineIndex}`)
}

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
  highlightTestId?: string
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
  highlightTestId,
}: CodeEditorProps) {
  const [copied, setCopied] = React.useState(false)
  const textareaRef = React.useRef<HTMLTextAreaElement>(null)
  const lineNumbersRef = React.useRef<HTMLDivElement>(null)
  const highlightRef = React.useRef<HTMLPreElement>(null)

  const lines = value.split("\n")
  const lineCount = Math.max(1, lines.length)
  const showSyntaxHighlight = supportsSyntaxHighlight(language)

  // Sync scroll between textarea and line numbers
  const handleScroll = (e: React.UIEvent<HTMLTextAreaElement>) => {
    if (lineNumbersRef.current) {
      lineNumbersRef.current.scrollTop = e.currentTarget.scrollTop
    }
    if (highlightRef.current) {
      highlightRef.current.scrollTop = e.currentTarget.scrollTop
      highlightRef.current.scrollLeft = e.currentTarget.scrollLeft
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
      <div className="flex" style={{ maxHeight, minHeight }}>
        {showLineNumbers && (
          <div
            ref={lineNumbersRef}
            className="select-none overflow-hidden bg-muted/30 text-muted-foreground text-xs font-mono py-2 px-2 text-right shrink-0 border-r"
            style={{ minWidth: "2.5rem" }}
          >
            {Array.from({ length: lineCount }, (_, i) => (
              <div key={i} className="leading-5">
                {i + 1}
              </div>
            ))}
          </div>
        )}
        <div className="relative flex-1 overflow-hidden">
          {showSyntaxHighlight ? (
            <pre
              ref={highlightRef}
              data-testid={highlightTestId}
              aria-hidden="true"
              className="pointer-events-none absolute inset-0 overflow-auto p-2 text-xs font-mono leading-5 whitespace-pre"
            >
              <code>
                {lines.map((line, index) => {
                  const lineKind = detectMarkdownLineKind(line)
                  return (
                    <div
                      key={`syntax-${index}`}
                      data-syntax-line={lineKind}
                      className={cn("leading-5", lineToneClass(lineKind))}
                    >
                      {renderMarkdownLine(line, index)}
                    </div>
                  )
                })}
              </code>
            </pre>
          ) : null}
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
            "h-full w-full bg-transparent p-2 text-xs font-mono resize-none outline-none",
            "leading-5 whitespace-pre",
            showSyntaxHighlight && "relative z-10 text-transparent caret-foreground placeholder:text-muted-foreground selection:bg-primary/20 selection:text-transparent",
            readOnly && "cursor-text"
          )}
          style={{ tabSize: 2 }}
        />
        </div>
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
