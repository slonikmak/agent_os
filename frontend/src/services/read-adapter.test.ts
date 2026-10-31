import test from 'node:test';
import assert from 'node:assert/strict';
import { ReadAdapter } from './read-adapter.ts';
import type { ReadResult, WorkspaceSnapshot } from './models.ts';

const snapshot = (revision: number): WorkspaceSnapshot => ({ workspaceId: 'persisted', workspaceRevision: revision, createdAt: '2026-10-29T00:00:00Z', tasks: [], inbox: [], sources: [], counts: { tasks: 0, inbox: 0, unprocessedInbox: 0, needsAttention: 0 } });
const result = (revision: number): ReadResult => ({ data: snapshot(revision), error: null });
function pending<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(done => { resolve = done; }); return { promise, resolve }; }
const settle = async () => { await Promise.resolve(); await Promise.resolve(); await Promise.resolve(); };

test('subscribes before reading and discards delayed older requests', async () => {
 const calls: ReturnType<typeof pending<ReadResult>>[] = [];
 let subscribed = false;
 const adapter = new ReadAdapter({ subscribe: () => { subscribed = true; return () => { subscribed = false; }; }, read: () => { assert.ok(subscribed); const call = pending<ReadResult>(); calls.push(call); return call.promise; } });
 adapter.start();
 assert.equal(adapter.getState().snapshot, null);
 const refresh = adapter.refresh();
 calls[1].resolve(result(3)); await refresh;
 calls[0].resolve(result(1)); await settle();
 assert.equal(adapter.getState().snapshot?.workspaceRevision, 3);
 adapter.stop(); assert.equal(subscribed, false);
});

test('coalesces invalidations, rejects snapshots below freshness floor and rereads', async () => {
 const calls: ReturnType<typeof pending<ReadResult>>[] = [];
 let invalidate!: (revision: number) => void;
 const adapter = new ReadAdapter({ subscribe: callback => { invalidate = callback; return () => {}; }, read: () => { const call = pending<ReadResult>(); calls.push(call); return call.promise; } });
 adapter.start(); calls[0].resolve(result(1)); await settle();
 invalidate(2); invalidate(3); invalidate(2); await settle();
 assert.equal(calls.length, 2);
 calls[1].resolve(result(2)); await settle();
 assert.equal(adapter.getState().snapshot?.workspaceRevision, 1);
 assert.equal(calls.length, 3);
 calls[2].resolve(result(3)); await settle();
 assert.equal(adapter.getState().snapshot?.workspaceRevision, 3);
 adapter.stop();
});

test('read failure preserves the last snapshot and reports a safe typed failure', async () => {
 let fail = false;
 const adapter = new ReadAdapter({ subscribe: () => () => {}, read: async () => { if (fail) throw new Error('TOKEN SQL /private'); return result(2); } });
 adapter.start(); await settle(); fail = true; await adapter.refresh();
 assert.equal(adapter.getState().snapshot?.workspaceRevision, 2);
 assert.equal(adapter.getState().error?.code, 'service_unavailable');
 assert.doesNotMatch(JSON.stringify(adapter.getState()), /TOKEN|SQL|private/);
 adapter.stop();
});

test('teardown prevents late reads and invalidations from changing the view', async () => {
 const call = pending<ReadResult>(); let invalidate!: (revision: number) => void; let unsubscribed = false;
 const adapter = new ReadAdapter({ read: () => call.promise, subscribe: callback => { invalidate = callback; return () => { unsubscribed = true; }; } });
 adapter.start(); adapter.stop(); call.resolve(result(4)); invalidate(5); await settle();
 assert.ok(unsubscribed); assert.equal(adapter.getState().snapshot, null);
});

const messageDetail = (id: string, revision: number): import('./models.ts').InboxResult => ({ data: { workspaceRevision: revision, message: { id, source: 'manual', scope: 'synthetic', kind: 'user_request', status: 'pending', sender: null, occurredAt: null, receivedAt: '2026-10-29T00:00:00Z', revision: 1, messageId: id, text: `Original ${id}`, sourceUrl: null, artifacts: [], submittedBy: { clientId: 'client-11111111111111111111111111111111', role: 'worker' }, proposedTaskRef: null, resolution: null } }, error: null });

test('selection changes discard late details and never reuse another selection on failure', async () => {
 const calls: { id: string; pending: ReturnType<typeof pending<import('./models.ts').InboxResult>> }[] = [];
 const adapter = new ReadAdapter({ read: async () => result(1), readInbox: id => { const request = pending<import('./models.ts').InboxResult>(); calls.push({ id, pending: request }); return request.promise; }, subscribe: () => () => {} });
 adapter.start(); await settle();
 adapter.selectInbox('I-one'); adapter.selectInbox('I-two');
 calls[0].pending.resolve(messageDetail('I-one', 1)); await settle();
 assert.equal(adapter.getState().inbox.detail, null);
 calls[1].pending.resolve(messageDetail('I-two', 1)); await settle();
 assert.equal(adapter.getState().inbox.detail?.message.id, 'I-two');
 adapter.selectInbox('I-three');
 assert.equal(adapter.getState().inbox.detail, null);
 calls[2].pending.resolve({ data: null, error: { code: 'service_unavailable', message: 'Unavailable', retryable: true, requestId: 'attempt', errorId: null, currentRevision: null } }); await settle();
 assert.equal(adapter.getState().inbox.detail, null);
 assert.equal(adapter.getState().inbox.error?.code, 'service_unavailable');
 adapter.stop();
});

test('detail invalidations coalesce and require the latest committed revision', async () => {
 const calls: ReturnType<typeof pending<import('./models.ts').InboxResult>>[] = [];
 let invalidate!: (revision: number) => void;
 let revision = 1;
 const adapter = new ReadAdapter({ read: async () => result(revision), readInbox: () => { const request = pending<import('./models.ts').InboxResult>(); calls.push(request); return request.promise; }, subscribe: callback => { invalidate = callback; return () => {}; } });
 adapter.start(); await settle(); adapter.selectInbox('I-one');
 revision = 4; invalidate(2); invalidate(4); invalidate(3); await settle();
 assert.equal(calls.length, 1);
 calls[0].resolve(messageDetail('I-one', 1)); await settle();
 assert.equal(adapter.getState().inbox.detail, null);
 assert.equal(calls.length, 2);
 calls[1].resolve(messageDetail('I-one', 4)); await settle();
 assert.equal(adapter.getState().inbox.detail?.workspaceRevision, 4);
 adapter.stop();
});

test('same-selection detail failure preserves saved text and teardown ignores late detail results', async () => {
 let fail = false;
 let late: ReturnType<typeof pending<import('./models.ts').InboxResult>> | null = null;
 const adapter = new ReadAdapter({ read: async () => result(2), readInbox: async id => { if (late) return late.promise; if (fail) throw new Error('PRIVATE_BODY TOKEN'); return messageDetail(id, 2); }, subscribe: () => () => {} });
 adapter.start(); await settle(); adapter.selectInbox('I-one'); await settle();
 fail = true; await adapter.refresh();
 assert.equal(adapter.getState().inbox.detail?.message.text, 'Original I-one');
 assert.equal(adapter.getState().inbox.error?.code, 'service_unavailable');
 assert.doesNotMatch(JSON.stringify(adapter.getState()), /PRIVATE_BODY|TOKEN/);
 late = pending<import('./models.ts').InboxResult>(); const refresh = adapter.refresh(); adapter.stop(); late.resolve(messageDetail('I-one', 5)); await refresh;
 assert.equal(adapter.getState().inbox.detail?.workspaceRevision, 2);
});

test('a newer snapshot and failed selected detail settle without repeated reads', async t => {
 for (const order of ['snapshot-first', 'detail-first']) {
  await t.test(order, async t => {
   const nextSnapshot = pending<ReadResult>();
   const nextDetail = pending<import('./models.ts').InboxResult>();
   const extraSnapshot = pending<ReadResult>();
   const extraDetail = pending<import('./models.ts').InboxResult>();
   let snapshotReads = 0, detailReads = 0;
   const adapter = new ReadAdapter({
    read: async () => {
     snapshotReads++;
     if (snapshotReads === 1) return result(2);
     // Unexpected retries remain pending, keeping this regression bounded even
     // if a broken adapter repeatedly schedules immediately completed reads.
     return snapshotReads === 2 ? nextSnapshot.promise : extraSnapshot.promise;
    },
    readInbox: async id => {
     detailReads++;
     if (detailReads === 1) return messageDetail(id, 2);
     return detailReads === 2 ? nextDetail.promise : extraDetail.promise;
    },
    subscribe: () => () => {},
   });
   t.after(() => adapter.stop());
   adapter.start(); await settle(); adapter.selectInbox('I-one'); await settle();
   const refresh = adapter.refresh();
   const failure: import('./models.ts').InboxResult = { data: null, error: { code: 'service_unavailable', message: 'Unavailable', retryable: true, requestId: 'attempt', errorId: null, currentRevision: null } };
   if (order === 'snapshot-first') {
    nextSnapshot.resolve(result(3)); await settle(); nextDetail.resolve(failure);
   } else {
    nextDetail.resolve(failure); await settle(); nextSnapshot.resolve(result(3));
   }
   await refresh; await settle(); await settle();
   const state = adapter.getState();
   assert.equal(state.snapshot?.workspaceRevision, 3);
   assert.equal(state.inbox.detail?.workspaceRevision, 2);
   assert.equal(state.inbox.detail?.message.text, 'Original I-one');
   assert.equal(state.inbox.error?.code, 'service_unavailable');
   assert.equal(state.loading, false);
   assert.equal(state.inbox.loading, false);
   assert.equal(snapshotReads, 2);
   assert.equal(detailReads, 2);
  });
 }
});

test('a superseded same-selection detail cannot overwrite a newer read', async () => {
 const calls: ReturnType<typeof pending<import('./models.ts').InboxResult>>[] = [];
 const adapter = new ReadAdapter({ read: async () => result(2), readInbox: () => { const request = pending<import('./models.ts').InboxResult>(); calls.push(request); return request.promise; }, subscribe: () => () => {} });
 adapter.start(); await settle(); adapter.selectInbox('I-one');
 const refresh = adapter.refresh();
 calls[1].resolve(messageDetail('I-one', 4)); await refresh;
 calls[0].resolve(messageDetail('I-one', 2)); await settle();
 assert.equal(adapter.getState().inbox.detail?.workspaceRevision, 4);
 adapter.stop();
});

test('correlated bridge reporting preserves saved state and ignores completions after teardown', async () => {
 const fault=pending<import('./models.ts').ReadFailure>();let fail=false;
 const adapter=new ReadAdapter({subscribe:()=>()=>{},read:async()=>{if(fail)throw new Error('SYNTHETIC_SECRET /private');return result(2);},bridgeFailure:()=>fault.promise});
 adapter.start();await settle();fail=true;const refresh=adapter.refresh();await settle();adapter.stop();
 const before=adapter.getState();fault.resolve({code:'service_unavailable',message:'The workspace is unavailable',retryable:true,requestId:'b'.repeat(32),errorId:'a'.repeat(32),currentRevision:null});await refresh;
 assert.equal(adapter.getState(),before);assert.equal(adapter.getState().snapshot?.workspaceRevision,2);
});
