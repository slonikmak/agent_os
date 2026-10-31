import test from 'node:test';
import assert from 'node:assert/strict';
import { ReadAdapter } from './read-adapter.ts';
import type { InboxResult, ReadResult, TaskResult } from './models.ts';

function pending<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(done => { resolve = done; }); return { promise, resolve }; }
const settle = async () => { for (let n = 0; n < 8; n++) await Promise.resolve(); };
const snapshot = (revision: number): ReadResult => ({ data: { workspaceId: 'saved', workspaceRevision: revision, createdAt: '2026-10-29T00:00:00Z', tasks: [], inbox: [], sources: [], counts: { tasks: 0, inbox: 0, unprocessedInbox: 0, needsAttention: 0 } }, error: null });
const task = (id: string, revision: number): TaskResult => ({ data: { workspaceRevision: revision, task: { id, revision: 1, kind: 'request', status: 'new', title: `Saved ${id}`, summary: null, nextAction: null, nextActor: null, createdAt: '2026-10-29T00:00:00Z', updatedAt: '2026-10-29T00:00:00Z', provenance: null, timeline: [], materials: [] } }, error: null });
const unavailable: TaskResult = { data: null, error: { code: 'service_unavailable', message: 'Unavailable', retryable: true, requestId: 'attempt', errorId: null, currentRevision: null } };
const inbox = (revision: number): InboxResult => ({ data: { workspaceRevision: revision, message: { id: 'I-one', source: 'manual', scope: 'test', kind: 'user_request', status: 'pending', sender: null, occurredAt: null, receivedAt: '2026-10-29T00:00:00Z', revision: 1, messageId: 'saved', text: 'Original', sourceUrl: null, artifacts: [], submittedBy: { clientId: 'client-11111111111111111111111111111111', role: 'worker' }, proposedTaskRef: null, resolution: null } }, error: null });

test('task selection change, close and reopen discard every previous generation', async () => {
 const calls: { id: string; request: ReturnType<typeof pending<TaskResult>> }[] = [];
 const adapter = new ReadAdapter({ read: async () => snapshot(1), readTask: id => { const request = pending<TaskResult>(); calls.push({ id, request }); return request.promise; }, subscribe: () => () => {} });
 adapter.start(); await settle(); adapter.selectTask('T-one'); adapter.selectTask('T-two');
 calls[0].request.resolve(task('T-one', 2)); await settle(); assert.equal(adapter.getState().task.detail, null);
 calls[1].request.resolve(task('T-two', 2)); await settle(); assert.equal(adapter.getState().task.detail?.task.id, 'T-two');
 adapter.selectTask('T-one'); adapter.selectTask(null); adapter.selectTask('T-one');
 calls[2].request.resolve(task('T-one', 4)); await settle(); assert.equal(adapter.getState().task.detail, null);
 calls[3].request.resolve(unavailable); await settle(); assert.equal(adapter.getState().task.detail, null); assert.equal(adapter.getState().task.error?.code, 'service_unavailable');
 adapter.stop();
});

test('superseded task refresh cannot replace a newer saved detail', async () => {
 const calls: ReturnType<typeof pending<TaskResult>>[] = [];
 const adapter = new ReadAdapter({ read: async () => snapshot(1), readTask: () => { const request = pending<TaskResult>(); calls.push(request); return request.promise; }, subscribe: () => () => {} });
 adapter.start(); await settle(); adapter.selectTask('T-one'); const refresh = adapter.refresh();
 calls[1].resolve(task('T-one', 4)); await refresh; calls[0].resolve(task('T-one', 2)); await settle();
 assert.equal(adapter.getState().task.detail?.workspaceRevision, 4); adapter.stop();
});

test('task invalidation bursts coalesce and stale task rereads do not reread the workspace', async () => {
 const calls: ReturnType<typeof pending<TaskResult>>[] = []; let revision = 1, snapshotReads = 0; let invalidate!: (revision: number) => void;
 const adapter = new ReadAdapter({ read: async () => { snapshotReads++; return snapshot(revision); }, readTask: () => { const request = pending<TaskResult>(); calls.push(request); return request.promise; }, subscribe: callback => { invalidate = callback; return () => {}; } });
 adapter.start(); await settle(); adapter.selectTask('T-one'); revision = 4; invalidate(2); invalidate(4); invalidate(3); await settle();
 assert.equal(calls.length, 1); calls[0].resolve(task('T-one', 1)); await settle();
 assert.equal(adapter.getState().task.detail, null); assert.equal(calls.length, 2); assert.equal(snapshotReads, 2);
 calls[1].resolve(task('T-one', 4)); await settle(); assert.equal(adapter.getState().task.detail?.workspaceRevision, 4); adapter.stop();
});

test('a task detail below its previous revision is discarded even without invalidation', async () => {
 let reads = 0;
 const adapter = new ReadAdapter({ read: async () => snapshot(1), readTask: async id => task(id, ++reads === 2 ? 2 : 4), subscribe: () => () => {} });
 adapter.start(); await settle(); adapter.selectTask('T-one'); await settle(); await adapter.refresh(); await settle();
 assert.equal(adapter.getState().task.detail?.workspaceRevision, 4); assert.equal(reads, 3); adapter.stop();
});

test('a newer snapshot and a task failure settle in both completion orders, then explicit refresh retries', async t => {
 for (const order of ['snapshot-first', 'task-first']) await t.test(order, async () => {
  const nextSnapshot = pending<ReadResult>(), nextTask = pending<TaskResult>(), extraTask = pending<TaskResult>(); let snapshotReads = 0, taskReads = 0;
  const adapter = new ReadAdapter({ read: async () => ++snapshotReads === 1 ? snapshot(2) : nextSnapshot.promise, readTask: async id => ++taskReads === 1 ? task(id, 2) : taskReads === 2 ? nextTask.promise : extraTask.promise, subscribe: () => () => {} });
  adapter.start(); await settle(); adapter.selectTask('T-one'); await settle(); const refresh = adapter.refresh();
  if (order === 'snapshot-first') { nextSnapshot.resolve(snapshot(3)); await settle(); nextTask.resolve(unavailable); }
  else { nextTask.resolve(unavailable); await settle(); nextSnapshot.resolve(snapshot(3)); }
  await refresh; await settle();
  assert.equal(adapter.getState().snapshot?.workspaceRevision, 3); assert.equal(adapter.getState().task.detail?.task.title, 'Saved T-one'); assert.equal(adapter.getState().task.detail?.workspaceRevision, 2);
  assert.equal(adapter.getState().task.error?.code, 'service_unavailable'); assert.equal(adapter.getState().task.loading, false); assert.equal(taskReads, 2); assert.equal(snapshotReads, 2);
  const retry = adapter.refresh(); extraTask.resolve(task('T-one', 3)); await retry; assert.equal(adapter.getState().task.error, null); adapter.stop();
 });
});

test('concurrent Inbox stale completion cannot clear or retry a settled task error', async () => {
 let revision = 1, taskReads = 0, inboxReads = 0; let invalidate!: (revision: number) => void;
 const staleInbox = pending<InboxResult>();
 const adapter = new ReadAdapter({ read: async () => snapshot(revision), readTask: async id => ++taskReads === 1 ? task(id, 1) : unavailable, readInbox: async () => ++inboxReads === 1 ? inbox(1) : inboxReads === 2 ? staleInbox.promise : inbox(3), subscribe: callback => { invalidate = callback; return () => {}; } });
 adapter.start(); await settle(); adapter.selectTask('T-one'); adapter.selectInbox('I-one'); await settle();
 revision = 3; invalidate(3); await settle(); assert.equal(adapter.getState().task.error?.code, 'service_unavailable');
 staleInbox.resolve(inbox(2)); await settle(); assert.equal(adapter.getState().inbox.detail?.workspaceRevision, 3); assert.equal(adapter.getState().task.error?.code, 'service_unavailable'); assert.equal(taskReads, 2); assert.equal(inboxReads, 3); adapter.stop();
});

test('task response identity and invalid revisions fail safely without automatic retry', async () => {
 for (const response of [task('T-other', 2), task('T-one', 0), task('T-one', Number.MAX_SAFE_INTEGER + 1)]) {
  let calls = 0;
  const adapter = new ReadAdapter({ read: async () => snapshot(1), readTask: async () => { calls++; return response; }, subscribe: () => () => {} });
  adapter.start(); await settle(); adapter.selectTask('T-one'); await settle(); assert.equal(adapter.getState().task.detail, null); assert.equal(adapter.getState().task.error?.code, 'service_unavailable'); assert.equal(calls, 1); adapter.stop();
 }
});

test('task bridge exceptions preserve saved details and shutdown ignores all late results', async () => {
 let mode = 'saved', unsubscribed = false; const late = pending<TaskResult>(); let invalidate!: (revision: number) => void;
 const adapter = new ReadAdapter({ read: async () => snapshot(2), readTask: async id => { if (mode === 'fail') throw new Error('PRIVATE SQL TOKEN'); if (mode === 'late') return late.promise; return task(id, 2); }, subscribe: callback => { invalidate = callback; return () => { unsubscribed = true; }; } });
 adapter.start(); await settle(); adapter.selectTask('T-one'); await settle(); mode = 'fail'; await adapter.refresh(); assert.equal(adapter.getState().task.detail?.workspaceRevision, 2); assert.doesNotMatch(JSON.stringify(adapter.getState()), /PRIVATE|SQL|TOKEN/);
 mode = 'late'; const refresh = adapter.refresh(); adapter.stop(); late.resolve(task('T-one', 4)); invalidate(5); await refresh; await settle(); assert.equal(adapter.getState().task.detail?.workspaceRevision, 2); assert.ok(unsubscribed);
});
