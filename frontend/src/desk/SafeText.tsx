import { useState } from 'react';
import { safeBrowserURL, textLinks } from '../services/safe-links';
import { openSafeURL } from '../services/navigation';
export function SafeLink({ value, label }: { value: string; label?: string }) {
 const [failed, setFailed] = useState(false);
 const target = safeBrowserURL(value);
 if (!target) return <>{label ?? value}</>;
 return <><button type="button" role="link" tabIndex={0} className="pd-safe-link" title="Открыть в браузере" onClick={() => { void openSafeURL(target).then(opened => setFailed(!opened)); }}>{label ?? value}</button>{failed && <small role="status">Не удалось открыть браузер.</small>}</>;
}
// JSX text nodes escape HTML and Markdown. No resource-rendering element is
// created from saved content; links invoke the native allowlist only on activation.
export function SafeText({ text }: { text: string }) {
 return <>{textLinks(text).map((part, index) => part.url ? <SafeLink key={index} value={part.url} label={part.text}/> : <span key={index}>{part.text}</span>)}</>;
}
