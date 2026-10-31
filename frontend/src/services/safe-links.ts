// Keep this parser conservative; the native facade independently enforces the
// allowlist immediately before the operating-system browser call.
export function safeBrowserURL(value: string): string | null {
 if (!value || value.length > 8192 || value.includes('\\')) return null;
 try { if (/[\s\p{Cc}\\]/u.test(decodeURIComponent(value))) return null; } catch { return null; }
 const match = /^(https?):\/\/([^/?#]+)/i.exec(value);
 if (!match || match[2].includes('@')) return null;
 const authority = match[2];
 let host: string; let port: string | undefined;
 if (authority.startsWith('[')) {
  const parts = /^\[([a-f\d:.]+)\](?::(\d+))?$/i.exec(authority);
  if (!parts) return null;
  host = parts[1]; port = parts[2];
 } else {
  const parts = /^([^:]+)(?::(\d+))?$/.exec(authority);
  if (!parts) return null;
  host = parts[1]; port = parts[2];
  if (host.length > 253 || host.split('.').some(label => !/^[a-z\d](?:[a-z\d-]{0,61}[a-z\d])?$/i.test(label))) return null;
  if (/^[\d.]+$/.test(host) && (host.split('.').length !== 4 || host.split('.').some(part => !/^(0|[1-9]\d{0,2})$/.test(part) || Number(part) > 255))) return null;
 }
 if (port !== undefined && (Number(port) < 1 || Number(port) > 65535)) return null;
 try { const url = new URL(value); if (url.username || url.password || !url.hostname || (!authority.startsWith('[') && url.hostname.toLowerCase() !== host.toLowerCase())) return null; return url.href; } catch { return null; }
}
export interface TextPart { text: string; url: string | null }
export function textLinks(text: string): TextPart[] {
 const parts: TextPart[] = []; let cursor = 0;
 for (const match of text.matchAll(/https?:\/\/[^\s<>"`]+/gi)) {
  const offset = match.index; let candidate = match[0];
  // Only an enclosing parenthesis supplies a boundary outside the URL. Keep
  // balanced URL parentheses, IPv6 brackets and valid trailing punctuation.
  if (text[offset - 1] === '(') {
   let depth = 0;
   for (let index = 0; index < candidate.length; index++) {
    if (candidate[index] === '(') depth++;
    else if (candidate[index] === ')') {
     if (depth === 0) { candidate = candidate.slice(0, index); break; }
     depth--;
    }
   }
  }
  const url = safeBrowserURL(candidate);
  if (!url) continue;
  if (offset > cursor) parts.push({ text: text.slice(cursor, offset), url: null });
  parts.push({ text: candidate, url }); cursor = offset + candidate.length;
 }
 if (cursor < text.length || !parts.length) parts.push({ text: text.slice(cursor), url: null });
 return parts;
}
