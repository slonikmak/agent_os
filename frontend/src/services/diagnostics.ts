import type { FrontendFault, ReadFailure } from './models.ts';
export interface FaultResult { status: string; requestId: string; errorId: string | null }
export type FaultCode = FrontendFault['code'];
export type FaultComponent = FrontendFault['component'];
export type FaultRequest = Promise<FaultResult> & { cancel?(): void | PromiseLike<void> };
const unavailable = (): FaultResult => ({ status: 'unavailable', requestId: '', errorId: null });
const validID = (id: unknown): id is string => typeof id === 'string' && /^[0-9a-f]{32}$/.test(id);
const codes = new Set(['ui.render_fault', 'ui.runtime_fault', 'ui.promise_fault']);
const components = new Set(['desk', 'board', 'inbox', 'task_drawer', 'read_adapter', 'root']);
interface Entry { at: number; result: Promise<FaultResult> }

// Fixed metadata only: callers cannot supply thrown objects, messages, stacks,
// props, paths or rejection payloads. Both sides independently enforce bounds.
export class DiagnosticReporter {
 private entries = new Map<string, Entry>();
 private times: number[] = [];
 private listeners = new Set<() => void>();
 private pending = new Set<FaultRequest>();
 private timeouts = new Set<() => void>();
 private closed = false;
 private notice: FaultResult | null = null;
 private send: (raw: string) => FaultRequest;
 private now: () => number;
 private timeoutMs: number;
 constructor(send: (raw: string) => FaultRequest, now = Date.now, timeoutMs = 1000) { this.send = send; this.now = now; this.timeoutMs = timeoutMs; }
 observe = (listener: () => void): (() => void) => { this.listeners.add(listener); return () => this.listeners.delete(listener); };
 getNotice = (): FaultResult | null => this.notice;
 private publish(value: FaultResult): FaultResult {
  if (!this.closed) {
   this.notice = value;
   this.listeners.forEach(listener => { try { listener(); } catch { /* Observer failures cannot recursively report. */ } });
  }
  return value;
 }
 private cancel(request: FaultRequest | undefined): void {
  try { if (request?.cancel) void Promise.resolve(request.cancel()).catch(() => {}); } catch { /* Shutdown/report cancellation is best effort. */ }
 }
 report = (code: FaultCode, component: FaultComponent): Promise<FaultResult> => {
  if (this.closed || !codes.has(code) || !components.has(component)) return Promise.resolve(unavailable());
  const now = this.now(); const key = `${code}:${component}`;
  const prior = this.entries.get(key);
  if (prior && now - prior.at < 60_000) return prior.result;
  this.times = this.times.filter(at => now - at < 60_000);
  // At most four underlying calls survive a broken/hung bridge; no deferred
  // retry queue is created. Cancellable Wails calls are cancelled on timeout.
  if (this.times.length >= 20 || this.pending.size >= 4) return Promise.resolve(unavailable());
  this.times.push(now);
  const report: FrontendFault = { schemaVersion: 1, code, component, frames: [] };
  let timer: ReturnType<typeof setTimeout> | undefined;
  let request: FaultRequest | undefined;
  let timeout!: () => void;
  let source: Promise<FaultResult>;
  try {
   request = this.send(JSON.stringify(report)); this.pending.add(request);
   source = Promise.resolve(request).finally(() => { if (request) this.pending.delete(request); });
  } catch { source = Promise.resolve(unavailable()); }
  const result = Promise.race([
   source,
   new Promise<FaultResult>(resolve => {
    timeout = () => { this.cancel(request); resolve(unavailable()); };
    this.timeouts.add(timeout); timer = setTimeout(timeout, this.timeoutMs);
   }),
  ]).then(value => {
   const safe = value && (value.status === 'accepted' || value.status === 'suppressed') && validID(value.errorId) && validID(value.requestId)
    ? { status: value.status, errorId: value.errorId, requestId: value.requestId } : unavailable();
   return this.publish(safe);
  }, () => this.publish(unavailable())).finally(() => { if (timer !== undefined) clearTimeout(timer); this.timeouts.delete(timeout); });
  this.entries.set(key, { at: now, result });
  return result;
 };
 bridgeFailure = async (): Promise<ReadFailure> => {
  const fault = await this.report('ui.runtime_fault', 'read_adapter');
  return { code: 'service_unavailable', message: 'The workspace is unavailable', retryable: true, requestId: fault.requestId, errorId: fault.errorId, currentRevision: null };
 };
 dispose(): void {
  this.closed = true; this.listeners.clear();
  this.timeouts.forEach(timeout => timeout()); this.timeouts.clear();
  this.pending.forEach(request => this.cancel(request));
 }
 install(target: Pick<Window, 'addEventListener' | 'removeEventListener'>): () => void {
  const error = (): void => { void this.report('ui.runtime_fault', 'root'); };
  const rejection = (): void => { void this.report('ui.promise_fault', 'root'); };
  target.addEventListener('error', error); target.addEventListener('unhandledrejection', rejection);
  return () => { target.removeEventListener('error', error); target.removeEventListener('unhandledrejection', rejection); };
 }
}
