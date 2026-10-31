import test from 'node:test';
import assert from 'node:assert/strict';
import { ReadAdapter } from './read-adapter.ts';
import type { MaterialReadResult, ReadResult, TaskResult } from './models.ts';
function pending<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(done => { resolve = done; }); return { promise, resolve }; }
const settle = async () => { for (let n = 0; n < 12; n++) await Promise.resolve(); };
const snapshot = (revision: number): ReadResult => ({ data: { workspaceId: 'saved', workspaceRevision: revision, createdAt: '2026-10-29T00:00:00Z', tasks: [], inbox: [], sources: [], counts: { tasks: 0, inbox: 0, unprocessedInbox: 0, needsAttention: 0 } }, error: null });
const task = (id: string, revision = 1): TaskResult => ({ data: { workspaceRevision: revision, task: { id, revision: 1, kind: 'request', status: 'new', title: id, summary: null, nextAction: null, nextActor: null, createdAt: '2026-10-29T00:00:00Z', updatedAt: '2026-10-29T00:00:00Z', provenance: null, timeline: [], materials: ['M-one', 'M-two'].map(materialId => ({ coordinate: { kind: 'reference', coordinate: 'material:' + materialId, label: null }, materialId, availability: 'available', unavailableReason: null })) } }, error: null });
const material = (id: string, revision = 1): MaterialReadResult => ({ data: { workspaceRevision: revision, material: { id, availability: 'available', mediaType: 'text/markdown', text: '<script>inert</script> 👋', unavailableReason: null } }, error: null });
const failure: MaterialReadResult = { data: null, error: { code: 'service_unavailable', message: 'Unavailable', retryable: true, requestId: 'attempt', errorId: null, currentRevision: null } };
test('material generations discard delayed selection, task switch, close/reopen and teardown', async () => {
 const calls: ReturnType<typeof pending<MaterialReadResult>>[] = [];
 const adapter = new ReadAdapter({ read: async () => snapshot(1), readTask: async id => task(id), readMaterial: () => { const request = pending<MaterialReadResult>(); calls.push(request); return request.promise; }, subscribe: () => () => {} });
 adapter.start(); await settle(); adapter.selectTask('T-one'); await settle(); adapter.selectMaterial('M-one'); adapter.selectMaterial('M-two');
 calls[0].resolve(material('M-one')); await settle(); assert.equal(adapter.getState().material.detail, null);
 calls[1].resolve(material('M-two')); await settle(); assert.equal(adapter.getState().material.detail?.material.id, 'M-two');
 adapter.selectTask('T-two'); assert.equal(adapter.getState().material.detail, null); await settle(); adapter.selectMaterial('M-one'); adapter.selectTask(null); adapter.selectTask('T-two'); await settle(); adapter.selectMaterial('M-one');
 calls[2].resolve(material('M-one', 9)); await settle(); assert.equal(adapter.getState().material.detail, null);
 adapter.stop(); calls[3].resolve(material('M-one', 10)); await settle(); assert.equal(adapter.getState().material.detail, null);
});
test('same-selection failure keeps saved content and waits for explicit refresh without a retry loop', async () => {
 let reads = 0; let fail = false;
 const adapter = new ReadAdapter({ read: async () => snapshot(1), readTask: async id => task(id), readMaterial: async id => { reads++; return fail ? failure : material(id); }, subscribe: () => () => {} });
 adapter.start(); await settle(); adapter.selectTask('T-one'); await settle(); adapter.selectMaterial('M-one'); await settle(); const saved = adapter.getState().material.detail;
 fail = true; await adapter.refresh(); await settle(); assert.equal(reads, 2); assert.deepEqual(adapter.getState().material.detail, saved); assert.equal(adapter.getState().material.error?.code, 'service_unavailable'); await settle(); assert.equal(reads, 2);
 fail = false; await adapter.refresh(); assert.equal(reads, 3); assert.equal(adapter.getState().material.error, null); adapter.stop();
});
test('material invalidation bursts coalesce and honor snapshot and task freshness floors independently', async () => {
 let revision = 1; let invalidate!: (revision: number) => void; let snapshots = 0; const calls: ReturnType<typeof pending<MaterialReadResult>>[] = [];
 const adapter = new ReadAdapter({ read: async () => { snapshots++; return snapshot(revision); }, readTask: async id => task(id, revision), readMaterial: () => { const request = pending<MaterialReadResult>(); calls.push(request); return request.promise; }, subscribe: callback => { invalidate = callback; return () => {}; } });
 adapter.start(); await settle(); adapter.selectTask('T-one'); await settle(); adapter.selectMaterial('M-one'); revision = 7; invalidate(3); invalidate(7); invalidate(5); await settle(); assert.equal(calls.length, 1);
 calls[0].resolve(material('M-one', 1)); await settle(); assert.equal(adapter.getState().material.detail, null); assert.equal(calls.length, 2); assert.equal(snapshots, 2);
 calls[1].resolve(material('M-one', 7)); await settle(); assert.equal(adapter.getState().material.detail?.workspaceRevision, 7); adapter.stop();
});
test('wrong material identity and invalid revision become stable safe failures', async () => {
 for (const result of [material('M-other'), material('M-one', 0), material('M-one', NaN)]) {
  const adapter = new ReadAdapter({ read: async () => snapshot(1), readTask: async id => task(id), readMaterial: async () => result, subscribe: () => () => {} });
  adapter.start(); await settle(); adapter.selectTask('T-one'); await settle(); adapter.selectMaterial('M-one'); await settle(); assert.equal(adapter.getState().material.detail, null); assert.equal(adapter.getState().material.error?.code, 'service_unavailable'); adapter.stop();
 }
});
test('a task response arriving after material schedules only a material reread', async () => {
 const tasks: ReturnType<typeof pending<TaskResult>>[] = []; let reads = 0, snapshots = 0;
 const adapter = new ReadAdapter({ read: async () => { snapshots++; return snapshot(1); }, readTask: () => { const request = pending<TaskResult>(); tasks.push(request); return request.promise; }, readMaterial: async id => material(id, ++reads <= 2 ? 1 : 8), subscribe: () => () => {} });
 adapter.start(); await settle(); adapter.selectTask('T-one'); tasks[0].resolve(task('T-one')); await settle(); adapter.selectMaterial('M-one'); await settle(); const refresh = adapter.refresh(); await settle(); tasks[1].resolve(task('T-one', 8)); await refresh; await settle(); assert.equal(reads, 3); assert.equal(snapshots, 2); assert.equal(adapter.getState().material.detail?.workspaceRevision, 8); adapter.stop();
});
