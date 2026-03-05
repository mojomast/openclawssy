function escapeHTML(value) {
  return String(value || "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

function renderInline(text) {
  let out = escapeHTML(text);
  out = out.replace(/`([^`]+)`/g, "<code>$1</code>");
  out = out.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
  out = out.replace(/\[([^\]]+)\]\(([^)]+)\)/g, (_match, label, href) => `<a href="${escapeHTML(href)}" target="_blank" rel="noopener noreferrer">${escapeHTML(label)}</a>`);
  return out;
}

export function renderMarkdownToFragment(markdown) {
  const fragment = document.createDocumentFragment();
  const lines = String(markdown || "").replace(/\r/g, "").split("\n");
  let index = 0;

  const flushParagraph = (buffer) => {
    if (!buffer.length) return;
    const p = document.createElement("p");
    p.innerHTML = renderInline(buffer.join(" "));
    fragment.append(p);
    buffer.length = 0;
  };

  const paragraph = [];

  while (index < lines.length) {
    const line = lines[index];
    if (!line.trim()) {
      flushParagraph(paragraph);
      index += 1;
      continue;
    }
    if (line.startsWith("```")) {
      flushParagraph(paragraph);
      const codeLines = [];
      index += 1;
      while (index < lines.length && !lines[index].startsWith("```")) {
        codeLines.push(lines[index]);
        index += 1;
      }
      const pre = document.createElement("pre");
      const code = document.createElement("code");
      code.textContent = codeLines.join("\n");
      pre.append(code);
      fragment.append(pre);
      index += 1;
      continue;
    }
    const heading = line.match(/^(#{1,3})\s+(.*)$/);
    if (heading) {
      flushParagraph(paragraph);
      const tag = `h${Math.min(heading[1].length + 1, 4)}`;
      const el = document.createElement(tag);
      el.innerHTML = renderInline(heading[2]);
      fragment.append(el);
      index += 1;
      continue;
    }
    if (/^>\s+\[!/.test(line)) {
      flushParagraph(paragraph);
      const kind = (line.match(/^>\s+\[!([A-Z]+)\]/) || [])[1] || "INFO";
      const card = document.createElement("aside");
      card.className = `help-callout ${kind.toLowerCase()}`;
      const body = [];
      index += 1;
      while (index < lines.length && /^>\s?/.test(lines[index])) {
        body.push(lines[index].replace(/^>\s?/, ""));
        index += 1;
      }
      const title = document.createElement("strong");
      title.textContent = kind.charAt(0) + kind.slice(1).toLowerCase();
      const content = document.createElement("div");
      content.innerHTML = renderInline(body.join(" "));
      card.append(title, content);
      fragment.append(card);
      continue;
    }
    if (/^[-*]\s+/.test(line)) {
      flushParagraph(paragraph);
      const list = document.createElement("ul");
      while (index < lines.length && /^[-*]\s+/.test(lines[index])) {
        const li = document.createElement("li");
        li.innerHTML = renderInline(lines[index].replace(/^[-*]\s+/, ""));
        list.append(li);
        index += 1;
      }
      fragment.append(list);
      continue;
    }
    if (/^\d+\.\s+/.test(line)) {
      flushParagraph(paragraph);
      const list = document.createElement("ol");
      while (index < lines.length && /^\d+\.\s+/.test(lines[index])) {
        const li = document.createElement("li");
        li.innerHTML = renderInline(lines[index].replace(/^\d+\.\s+/, ""));
        list.append(li);
        index += 1;
      }
      fragment.append(list);
      continue;
    }
    paragraph.push(line.trim());
    index += 1;
  }

  flushParagraph(paragraph);
  return fragment;
}
