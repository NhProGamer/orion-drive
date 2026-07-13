// A small, dependency-free Markdown renderer. It escapes all HTML first, then
// applies a limited, safe subset of Markdown, so the output is XSS-safe to use
// with v-html. It is intentionally minimal (headings, emphasis, code, lists,
// links, blockquotes, rules) — enough for previewing notes.

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

function inline(s: string): string {
  return s
    .replace(/`([^`]+)`/g, '<code>$1</code>')
    .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
    .replace(/(^|[^*])\*([^*]+)\*/g, '$1<em>$2</em>')
    // Links are limited to http(s) targets; the URL is already HTML-escaped.
    .replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g, '<a href="$2" rel="noopener noreferrer" target="_blank">$1</a>')
}

/** Render a Markdown string to safe HTML. */
export function renderMarkdown(src: string): string {
  const lines = escapeHtml(src).split(/\r?\n/)
  const out: string[] = []
  let inCode = false
  let listOpen = false

  const closeList = () => {
    if (listOpen) {
      out.push('</ul>')
      listOpen = false
    }
  }

  for (const raw of lines) {
    const line = raw

    if (/^```/.test(line.trim())) {
      if (inCode) {
        out.push('</code></pre>')
        inCode = false
      } else {
        closeList()
        out.push('<pre><code>')
        inCode = true
      }
      continue
    }
    if (inCode) {
      out.push(line + '\n')
      continue
    }

    const h = line.match(/^(#{1,6})\s+(.*)$/)
    if (h) {
      closeList()
      const level = h[1].length
      out.push(`<h${level}>${inline(h[2])}</h${level}>`)
      continue
    }
    if (/^\s*([-*+])\s+/.test(line)) {
      if (!listOpen) {
        out.push('<ul>')
        listOpen = true
      }
      out.push(`<li>${inline(line.replace(/^\s*([-*+])\s+/, ''))}</li>`)
      continue
    }
    if (/^\s*>\s?/.test(line)) {
      closeList()
      out.push(`<blockquote>${inline(line.replace(/^\s*>\s?/, ''))}</blockquote>`)
      continue
    }
    if (/^\s*([-*_])\s*\1\s*\1[\s\S]*$/.test(line.trim())) {
      closeList()
      out.push('<hr>')
      continue
    }
    if (line.trim() === '') {
      closeList()
      continue
    }
    closeList()
    out.push(`<p>${inline(line)}</p>`)
  }
  if (inCode) out.push('</code></pre>')
  closeList()
  return out.join('\n')
}
