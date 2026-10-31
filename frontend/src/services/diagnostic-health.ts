export interface DiagnosticHealth { degraded: boolean; restartRequired: boolean }
export type HealthRequest = Promise<DiagnosticHealth> & { cancel?(): void | PromiseLike<void> };

// Poll only the existing operational status service. One underlying request is
// allowed at a time, including when a broken bridge ignores cancellation.
export function observeDiagnosticHealth(
 read: () => HealthRequest,
 changed: (health: DiagnosticHealth) => void,
 intervalMs = 2000,
 timeoutMs = 1000,
): () => void {
 let active = true;
 let pending: HealthRequest | undefined;
 let next: ReturnType<typeof setTimeout> | undefined;
 let deadline: ReturnType<typeof setTimeout> | undefined;
 const cancel = (request: HealthRequest | undefined): void => {
  try { if (request?.cancel) void Promise.resolve(request.cancel()).catch(() => {}); } catch { /* Operational bridge cancellation is best effort. */ }
 };
 const schedule = (): void => { if (active) next = setTimeout(poll, intervalMs); };
 const poll = (): void => {
  if (!active || pending) return;
  let request: HealthRequest;
  try { request = read(); pending = request; } catch { schedule(); return; }
  let timedOut = false;
  deadline = setTimeout(() => { timedOut = true; cancel(request); }, timeoutMs);
  void Promise.resolve(request).then(health => {
   if (active && !timedOut && typeof health?.degraded === 'boolean' && typeof health?.restartRequired === 'boolean') {
    try { changed({ degraded: health.degraded, restartRequired: health.restartRequired }); } catch { /* No recursive reporting from UI observers. */ }
   }
  }, () => {}).finally(() => {
   if (deadline !== undefined) clearTimeout(deadline);
   pending = undefined;
   schedule();
  });
 };
 poll();
 return () => {
  active = false;
  if (next !== undefined) clearTimeout(next);
  if (deadline !== undefined) clearTimeout(deadline);
  cancel(pending);
 };
}
