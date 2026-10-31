import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import type { WorkspaceSnapshot as NativeSnapshot } from '../../bindings/personaldesk/internal/adapters/wailsread/models';
import { mapWorkspace } from './native-models.ts';
import { categoryForKind } from './models.ts';

const fixture = (): NativeSnapshot => JSON.parse(readFileSync(new URL('../../../internal/contracts/v1/examples/workspace-populated.json', import.meta.url), 'utf8')) as NativeSnapshot;
test('schema-evaluated populated facts map into native view with explicit nulls', () => {
 const source = fixture(); const mapped = mapWorkspace(source);
 assert.deepEqual(mapped, source);
 assert.equal(mapped.tasks[0].summary, null); assert.equal(mapped.inbox[0].occurredAt, null);
 assert.equal(mapped.sources[0].lastAttemptAt, null);
 assert.equal(mapped.workspaceRevision, 3); assert.equal(mapped.counts.unprocessedInbox, 1);
 assert.deepEqual(categoryForKind, { task_analysis: 'development', code_review: 'review', request: 'other' });
});
test('unsupported native enums fail with safe fixed text', () => {
 for (const field of ['task', 'inbox', 'source'] as const) {
  const source = fixture();
  assert.ok(source.tasks && source.inbox && source.sources);
  if (field === 'task') source.tasks[0].kind = 'PRIVATE';
  if (field === 'inbox') source.inbox[0].status = 'PRIVATE';
  if (field === 'source') source.sources[0].status = 'PRIVATE';
  assert.throws(() => mapWorkspace(source), { message: 'Unsupported workspace response' });
 }
});

test('Inbox native mapping preserves original text, claimed source and authenticated provenance', async () => {
 const { mapInbox } = await import('./native-models.ts');
 const source = JSON.parse(readFileSync(new URL('../../../internal/contracts/v1/examples/inbox-details.json', import.meta.url), 'utf8')) as import('../../bindings/personaldesk/internal/adapters/wailsread/models').InboxDetails;
 assert.deepEqual(mapInbox(source), source);
 source.message.submittedBy.role = 'PRIVATE';
 assert.throws(() => mapInbox(source), { message: 'Unsupported Inbox response' });
});

test('task mapping preserves saved creation, original submitter, coordinator history, nulls and inert materials', async () => {
 const { mapTask } = await import('./native-models.ts');
 const source = JSON.parse(readFileSync(new URL('../../../internal/contracts/v1/examples/task-details.json', import.meta.url), 'utf8')) as import('../../bindings/personaldesk/internal/adapters/wailsread/models').TaskDetails;
 assert.deepEqual(mapTask(source), source);
 assert.notEqual(source.task.provenance?.submittedBy.clientId, source.task.timeline?.[0].appliedBy.clientId);
 source.task.timeline![0].appliedBy.role = 'PRIVATE';
 assert.throws(() => mapTask(source), { message: 'Unsupported task response' });
});
