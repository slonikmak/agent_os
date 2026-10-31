import test from 'node:test';
import assert from 'node:assert/strict';
import { safeBrowserURL, textLinks } from './safe-links.ts';
import { mapMaterialResult } from './native-models.ts';
test('shared links accept deliberate HTTP(S) targets and reject ambiguous schemes, hosts and controls', () => {
 for (const url of ['https://example.invalid/path?q=1#anchor', 'HTTP://localhost:1234/', 'http://127.0.0.1/', 'https://[::1]:443/']) assert.ok(safeBrowserURL(url), url);
 for (const url of ['', '/private/path', 'file:///etc/passwd', 'javascript:alert(1)', '//example.invalid', 'https://user:secret@example.invalid', 'https://example.invalid:0/', 'https://example.invalid:65536/', 'https://example.invalid:/', 'https://example.invalid\n/', 'https://example.invalid/%0a', 'https://example.invalid/%5c', 'https://exa mple.invalid/', 'https://127.1/', 'https://0x7f000001/', 'https://2130706433/', 'https://.example.invalid/', 'https://example..invalid/', 'https://-bad.invalid/', 'https://example.invalid\\@other.invalid/', 'https:///example.invalid', 'https://[::1%25en0]/']) assert.equal(safeBrowserURL(url), null, url);
});
test('untrusted HTML/Markdown stays exact text and creates no automatic resource requests', () => {
 const original = '<script>alert(1)</script> ![image](https://example.invalid/image) [link](https://example.invalid/page) file:///private\n';
 const parts = textLinks(original);
 assert.equal(parts.map(part => part.text).join(''), original);
 assert.equal(parts.filter(part => part.url).length, 2);
 assert.equal(parts[0].url, null);
});
test('text links preserve URL punctuation and IPv6 while excluding surrounding Markdown parentheses', () => {
 for (const { text, visible, target } of [
  { text: 'https://example.invalid/a(b)', visible: 'https://example.invalid/a(b)', target: 'https://example.invalid/a(b)' },
  { text: 'https://[::1]', visible: 'https://[::1]', target: 'https://[::1]/' },
  { text: '[link](https://example.invalid/a(b))', visible: 'https://example.invalid/a(b)', target: 'https://example.invalid/a(b)' },
  { text: 'Before [link](https://example.invalid/a(b)). After', visible: 'https://example.invalid/a(b)', target: 'https://example.invalid/a(b)' },
  { text: '(https://[::1])', visible: 'https://[::1]', target: 'https://[::1]/' },
  { text: 'https://example.invalid/a.', visible: 'https://example.invalid/a.', target: 'https://example.invalid/a.' },
  { text: 'https://example.invalid/a!;?value=done,', visible: 'https://example.invalid/a!;?value=done,', target: 'https://example.invalid/a!;?value=done,' },
 ]) {
  const parts = textLinks(text);
  assert.equal(parts.map(part => part.text).join(''), text);
  assert.deepEqual(parts.filter(part => part.url), [{ text: visible, url: target }], text);
 }
});
test('native material mapping validates unavailable nulls and supported exact Unicode', () => {
 assert.equal(mapMaterialResult({ workspaceRevision: 2, material: { id: 'M-one', availability: 'available', mediaType: 'text/plain', text: '👋', unavailableReason: null } }).material.text, '👋');
 for (const material of [ { id: 'M-one', availability: 'unavailable', mediaType: 'text/plain', text: 'PRIVATE', unavailableReason: 'missing' }, { id: 'M-one', availability: 'available', mediaType: 'text/html', text: 'PRIVATE', unavailableReason: null } ]) assert.throws(() => mapMaterialResult({ workspaceRevision: 2, material }), { message: 'Unsupported material response' });
});
