function escapeHTML(value: string): string {
  return String(value || "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/\"/g, "&quot;")
    .replace(/'/g, "&#39;")
}

function slugify(text: string): string {
  return (
    String(text || "")
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "") || "section"
  )
}

function renderInline(text: string): string {
  let out = escapeHTML(text)
  out = out.replace(/`([^`]+)`/g, "<code>$1</code>")
  out = out.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>")
  out = out.replace(/\[([^\]]+)\]\(([^)]+)\)/g, (_match, label: string, href: string) => {
    const safeHref = escapeHTML(href)
    const isInternal = safeHref.startsWith("#") || safeHref.startsWith("/")
    return `<a href="${safeHref}"${isInternal ? "" : ' target="_blank" rel="noopener noreferrer"'}>${escapeHTML(label)}</a>`
  })
  return out
}

export interface MarkdownHeading {
  level: number
  title: string
  id: string
}

export function extractHeadings(markdown: string): MarkdownHeading[] {
  const seen = new Map<string, number>()
  return String(markdown || "")
    .replace(/\r/g, "")
    .split("\n")
    .map((line) => line.match(/^(#{1,3})\s+(.*)$/))
    .filter((match): match is RegExpMatchArray => Boolean(match))
    .map((match) => {
      const title = String(match[2] || "").trim()
      const base = slugify(title)
      const count = seen.get(base) || 0
      seen.set(base, count + 1)
      return {
        level: Math.min(match[1].length + 1, 4),
        title,
        id: count ? `${base}-${count + 1}` : base,
      }
    })
}

function renderCallout(lines: string[], kind: string): HTMLElement {
  const card = document.createElement("aside")
  card.className = `help-callout ${kind.toLowerCase()}`
  const title = document.createElement("strong")
  title.textContent = kind.charAt(0) + kind.slice(1).toLowerCase()
  const content = document.createElement("div")
  content.innerHTML = renderInline(lines.join(" "))
  card.append(title, content)
  return card
}

function parseListBlock(lines: string[], startIndex: number, ordered: boolean): { node: HTMLOListElement | HTMLUListElement; nextIndex: number } {
  const root = document.createElement(ordered ? "ol" : "ul")
  const stack: Array<{ indent: number; list: HTMLOListElement | HTMLUListElement }> = [{ indent: -1, list: root }]
  let index = startIndex
  const marker = ordered ? /^\s*\d+\.\s+(.*)$/ : /^\s*[-*]\s+(.*)$/

  while (index < lines.length) {
    const line = lines[index]
    const match = line.match(marker)
    if (!match) {
      break
    }

    const indent = (line.match(/^\s*/) || [""])[0].length
    while (stack.length > 1 && indent <= stack[stack.length - 1].indent) {
      stack.pop()
    }

    const current = stack[stack.length - 1]
    const li = document.createElement("li")
    li.innerHTML = renderInline(match[1])
    current.list.append(li)

    const nextLine = lines[index + 1] || ""
    const nextUnordered = nextLine.match(/^\s*[-*]\s+/)
    const nextOrdered = nextLine.match(/^\s*\d+\.\s+/)
    if ((ordered && nextOrdered) || (!ordered && nextUnordered)) {
      const nextIndent = (nextLine.match(/^\s*/) || [""])[0].length
      if (nextIndent > indent) {
        const childList = document.createElement(ordered ? "ol" : "ul")
        li.append(childList)
        stack.push({ indent, list: childList })
      }
    }

    index += 1
  }

  return { node: root, nextIndex: index }
}

function isTableStart(lines: string[], index: number): boolean {
  const current = lines[index] || ""
  const next = lines[index + 1] || ""
  return /\|/.test(current) && /^\s*\|?\s*:?-{3,}/.test(next)
}

export function renderMarkdownToFragment(markdown: string): DocumentFragment {
  const fragment = document.createDocumentFragment()
  const lines = String(markdown || "").replace(/\r/g, "").split("\n")
  const headings = extractHeadings(markdown)
  let headingIndex = 0
  let index = 0
  const paragraph: string[] = []

  const flushParagraph = () => {
    if (!paragraph.length) {
      return
    }
    const p = document.createElement("p")
    p.innerHTML = renderInline(paragraph.join(" "))
    fragment.append(p)
    paragraph.length = 0
  }

  while (index < lines.length) {
    const line = lines[index]

    if (!line.trim()) {
      flushParagraph()
      index += 1
      continue
    }

    if (/^---\s*$/.test(line) || /^\*\*\*\s*$/.test(line)) {
      flushParagraph()
      fragment.append(document.createElement("hr"))
      index += 1
      continue
    }

    if (line.startsWith("```")) {
      flushParagraph()
      const codeLines: string[] = []
      index += 1
      while (index < lines.length && !lines[index].startsWith("```")) {
        codeLines.push(lines[index])
        index += 1
      }
      const pre = document.createElement("pre")
      const code = document.createElement("code")
      code.textContent = codeLines.join("\n")
      pre.append(code)
      fragment.append(pre)
      index += 1
      continue
    }

    const heading = line.match(/^(#{1,3})\s+(.*)$/)
    if (heading) {
      flushParagraph()
      const meta = headings[headingIndex] || {
        id: slugify(heading[2]),
        title: heading[2],
        level: Math.min(heading[1].length + 1, 4),
      }
      headingIndex += 1
      const el = document.createElement(`h${meta.level}`)
      el.id = meta.id
      el.innerHTML = renderInline(meta.title)
      fragment.append(el)
      index += 1
      continue
    }

    if (/^>\s+\[!/.test(line)) {
      flushParagraph()
      const kind = (line.match(/^>\s+\[!([A-Z]+)\]/) || [])[1] || "INFO"
      const body: string[] = []
      index += 1
      while (index < lines.length && /^>\s?/.test(lines[index])) {
        body.push(lines[index].replace(/^>\s?/, ""))
        index += 1
      }
      fragment.append(renderCallout(body, kind))
      continue
    }

    if (isTableStart(lines, index)) {
      flushParagraph()
      const tableLines = [lines[index], lines[index + 1]]
      index += 2
      while (index < lines.length && /\|/.test(lines[index])) {
        tableLines.push(lines[index])
        index += 1
      }
      fragment.append(renderCallout(["Table rendering is simplified in the dashboard Help Center. Raw table content is shown below."], "INFO"))
      const pre = document.createElement("pre")
      pre.textContent = tableLines.join("\n")
      fragment.append(pre)
      continue
    }

    if (/^\s*[-*]\s+/.test(line)) {
      flushParagraph()
      const { node, nextIndex } = parseListBlock(lines, index, false)
      fragment.append(node)
      index = nextIndex
      continue
    }

    if (/^\s*\d+\.\s+/.test(line)) {
      flushParagraph()
      const { node, nextIndex } = parseListBlock(lines, index, true)
      fragment.append(node)
      index = nextIndex
      continue
    }

    paragraph.push(line.trim())
    index += 1
  }

  flushParagraph()
  return fragment
}
