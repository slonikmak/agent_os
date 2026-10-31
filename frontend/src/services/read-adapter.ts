import type { MaterialResult, MaterialReadResult, InboxDetails, InboxResult, TaskDetails, TaskResult, ReadFailure, ReadResult, WorkspaceSnapshot } from './models.ts';

export interface ReadTransport {
 read(): Promise<ReadResult>;
 readInbox?(id: string): Promise<InboxResult>;
 readTask?(id: string): Promise<TaskResult>;
 readMaterial?(id: string): Promise<MaterialReadResult>;
 bridgeFailure?(): Promise<ReadFailure>;
 subscribe(callback: (revision: number) => void): () => void;
}
export interface InboxState {
 selectedId: string | null;
 detail: InboxDetails | null;
 loading: boolean;
 error: ReadFailure | null;
}
export interface ReadState {
 snapshot: WorkspaceSnapshot | null;
 loading: boolean;
 error: ReadFailure | null;
 inbox: InboxState;
 task: TaskState;
 material: MaterialState;
}
export interface TaskState { selectedId: string | null; detail: TaskDetails | null; loading: boolean; error: ReadFailure | null }
export interface MaterialState { selectedId: string | null; detail: MaterialResult | null; loading: boolean; error: ReadFailure | null }
const emptyMaterial = (): MaterialState => ({ selectedId: null, detail: null, loading: false, error: null });
const emptyInbox = (): InboxState => ({ selectedId: null, detail: null, loading: false, error: null });
const emptyTask = (): TaskState => ({ selectedId: null, detail: null, loading: false, error: null });
const bridgeFailure = (): ReadFailure => ({ code: 'service_unavailable', message: 'The workspace is unavailable', retryable: true, requestId: '', errorId: null, currentRevision: null });

// One adapter owns committed snapshots and selected details. View components
// retain navigation/filter state and never keep a second copy of domain data.
export class ReadAdapter {
 private transport: ReadTransport;
 private state: ReadState = { snapshot: null, loading: true, error: null, inbox: emptyInbox(), task: emptyTask(), material: emptyMaterial() };
 private observers = new Set<() => void>();
 private floor = 0;
 private generation = 0;
 private detailGeneration = 0;
 private taskGeneration = 0;
 private materialGeneration = 0;
 private stopped = true;
 private queued = false;
 private scheduled = new Set<'snapshot' | 'inbox' | 'task' | 'material'>();
 private unsubscribe: (() => void) | undefined;
 constructor(transport: ReadTransport) { this.transport = transport; }
 private failure = async (): Promise<ReadFailure> => { try { return await this.transport.bridgeFailure?.() ?? bridgeFailure(); } catch { return bridgeFailure(); } };
 getState = (): ReadState => this.state;
 observe = (callback: () => void): (() => void) => { this.observers.add(callback); return () => this.observers.delete(callback); };
 start(): void {
  if (!this.stopped) return;
  this.stopped = false;
  // Subscribe before the initial read so invalidations cannot disappear.
  try { this.unsubscribe = this.transport.subscribe(revision => {
   if (Number.isSafeInteger(revision) && revision > this.floor) { this.floor = revision; this.schedule(); }
  }); } catch { void this.failure(); }
  void this.refresh();
 }
 stop(): void { this.stopped = true; this.generation++; this.detailGeneration++; this.taskGeneration++; this.materialGeneration++; this.publish({ ...this.state, material: emptyMaterial() }); try { this.unsubscribe?.(); } catch { void this.failure(); } this.unsubscribe = undefined; }
 private publish(state: ReadState): void { this.state = state; this.observers.forEach(callback => callback()); }
 private schedule(query: 'all' | 'snapshot' | 'inbox' | 'task' | 'material' = 'all'): void {
  if (this.stopped) return;
  if (query === 'all') { this.scheduled.add('snapshot'); this.scheduled.add('inbox'); this.scheduled.add('task'); this.scheduled.add('material'); }
  else this.scheduled.add(query);
  if (this.queued) return;
  this.queued = true;
  queueMicrotask(() => {
   this.queued = false;
   const scheduled = this.scheduled; this.scheduled = new Set();
   if (this.stopped) return;
   // Invalidation bursts share an in-flight read; a stale completion schedules
   // one follow-up. Explicit user refreshes can supersede an older request.
   if (scheduled.has('snapshot') && !this.state.loading) void this.readSnapshot();
   if (scheduled.has('inbox') && !this.state.inbox.loading) void this.readInbox();
   if (scheduled.has('task') && !this.state.task.loading) void this.readTask();
   if (scheduled.has('material') && !this.state.material.loading) void this.readMaterial();
  });
 }
 refresh = async (): Promise<void> => { await Promise.all([this.readSnapshot(), this.readInbox(), this.readTask(), this.readMaterial()]); };
 selectTask = (id: string | null): void => {
  if (id === this.state.task.selectedId) return;
  this.taskGeneration++; this.materialGeneration++;
  this.publish({ ...this.state, task: { ...emptyTask(), selectedId: id }, material: emptyMaterial() });
  if (id) void this.readTask();
 };
 selectMaterial = (id: string | null): void => {
  if (id === this.state.material.selectedId) return;
  if (id && (!this.state.task.selectedId || !this.state.task.detail?.task.materials.some(item => item.materialId === id))) return;
  this.materialGeneration++;
  this.publish({ ...this.state, material: { ...emptyMaterial(), selectedId: id } });
  if (id) void this.readMaterial();
 };
 selectInbox = (id: string | null): void => {
  if (id === this.state.inbox.selectedId) return;
  this.detailGeneration++;
  this.publish({ ...this.state, inbox: { ...emptyInbox(), selectedId: id } });
  if (id) void this.readInbox();
 };
 private readSnapshot = async (): Promise<void> => {
  if (this.stopped) return;
  const generation = ++this.generation;
  this.publish({ ...this.state, loading: true, error: null });
  let result: ReadResult;
  try { result = await this.transport.read(); } catch { result = { data: null, error: await this.failure() }; }
  if (this.stopped || generation !== this.generation) return;
  if (result.error || !result.data) { this.publish({ ...this.state, loading: false, error: result.error ?? bridgeFailure() }); return; }
  const snapshot = result.data;
  if (!Number.isSafeInteger(snapshot.workspaceRevision) || snapshot.workspaceRevision < 1) {
   const error=await this.failure();if(this.stopped||generation!==this.generation)return;
   this.publish({ ...this.state, loading: false, error }); return;
  }
  if (snapshot.workspaceRevision < this.floor) { this.publish({ ...this.state, loading: false }); this.schedule('snapshot'); return; }
  if (snapshot.workspaceRevision < (this.state.snapshot?.workspaceRevision ?? 0)) { this.publish({ ...this.state, loading: false }); return; }
  this.publish({ ...this.state, snapshot, loading: false, error: null });
  if (this.state.material.detail && this.state.material.detail.workspaceRevision < snapshot.workspaceRevision && !this.state.material.loading && !this.state.material.error) void this.readMaterial();
  // A fresh snapshot can invalidate selected detail, but must not schedule
  // another snapshot or erase a settled detail failure by retrying it again.
  if (this.state.inbox.detail && this.state.inbox.detail.workspaceRevision < snapshot.workspaceRevision && !this.state.inbox.loading && !this.state.inbox.error) void this.readInbox();
  if (this.state.task.detail && this.state.task.detail.workspaceRevision < snapshot.workspaceRevision && !this.state.task.loading && !this.state.task.error) void this.readTask();
 };
 private readInbox = async (): Promise<void> => {
  const id = this.state.inbox.selectedId;
  if (this.stopped || !id || !this.transport.readInbox) return;
  const generation = ++this.detailGeneration;
  this.publish({ ...this.state, inbox: { ...this.state.inbox, loading: true, error: null } });
  let result: InboxResult;
  try { result = await this.transport.readInbox(id); } catch { result = { data: null, error: await this.failure() }; }
  if (this.stopped || generation !== this.detailGeneration || id !== this.state.inbox.selectedId) return;
  if (result.error || !result.data) { this.publish({ ...this.state, inbox: { ...this.state.inbox, loading: false, error: result.error ?? bridgeFailure() } }); return; }
  const detail = result.data;
  if (detail.message.id !== id || !Number.isSafeInteger(detail.workspaceRevision) || detail.workspaceRevision < 1) {
   const error=await this.failure();if(this.stopped||generation!==this.detailGeneration||id!==this.state.inbox.selectedId)return;
   this.publish({ ...this.state, inbox: { ...this.state.inbox, loading: false, error } }); return;
  }
  const floor = Math.max(this.floor, this.state.snapshot?.workspaceRevision ?? 0, this.state.inbox.detail?.workspaceRevision ?? 0);
  if (detail.workspaceRevision < floor) { this.publish({ ...this.state, inbox: { ...this.state.inbox, loading: false } }); this.schedule('inbox'); return; }
  this.publish({ ...this.state, inbox: { selectedId: id, detail, loading: false, error: null } });
 };
 private readTask = async (): Promise<void> => {
  const id = this.state.task.selectedId;
  if (this.stopped || !id || !this.transport.readTask) return;
  const generation = ++this.taskGeneration;
  this.publish({ ...this.state, task: { ...this.state.task, loading: true, error: null } });
  let result: TaskResult;
  try { result = await this.transport.readTask(id); } catch { result = { data: null, error: await this.failure() }; }
  if (this.stopped || generation !== this.taskGeneration || id !== this.state.task.selectedId) return;
  if (result.error || !result.data) { this.publish({ ...this.state, task: { ...this.state.task, loading: false, error: result.error ?? bridgeFailure() } }); return; }
  const detail = result.data;
  if (detail.task.id !== id || !Number.isSafeInteger(detail.workspaceRevision) || detail.workspaceRevision < 1) {
   const error=await this.failure();if(this.stopped||generation!==this.taskGeneration||id!==this.state.task.selectedId)return;
   this.publish({ ...this.state, task: { ...this.state.task, loading: false, error } }); return;
  }
  const floor = Math.max(this.floor, this.state.snapshot?.workspaceRevision ?? 0, this.state.task.detail?.workspaceRevision ?? 0);
  if (detail.workspaceRevision < floor) { this.publish({ ...this.state, task: { ...this.state.task, loading: false } }); this.schedule('task'); return; }
  this.publish({ ...this.state, task: { selectedId: id, detail, loading: false, error: null } });
  if (this.state.material.detail && this.state.material.detail.workspaceRevision < detail.workspaceRevision && !this.state.material.loading && !this.state.material.error) this.schedule('material');
 };
 private readMaterial = async (): Promise<void> => {
  const id = this.state.material.selectedId;
  if (this.stopped || !id || !this.transport.readMaterial) return;
  const generation = ++this.materialGeneration;
  this.publish({ ...this.state, material: { ...this.state.material, loading: true, error: null } });
  let result: MaterialReadResult;
  try { result = await this.transport.readMaterial(id); } catch { result = { data: null, error: await this.failure() }; }
  if (this.stopped || generation !== this.materialGeneration || id !== this.state.material.selectedId) return;
  if (result.error || !result.data) { this.publish({ ...this.state, material: { ...this.state.material, loading: false, error: result.error ?? bridgeFailure() } }); return; }
  const detail = result.data;
  if (detail.material.id !== id || !Number.isSafeInteger(detail.workspaceRevision) || detail.workspaceRevision < 1) {
   const error=await this.failure();if(this.stopped||generation!==this.materialGeneration||id!==this.state.material.selectedId)return;
   this.publish({ ...this.state, material: { ...this.state.material, loading: false, error } }); return;
  }
  const floor = Math.max(this.floor, this.state.snapshot?.workspaceRevision ?? 0, this.state.task.detail?.workspaceRevision ?? 0, this.state.material.detail?.workspaceRevision ?? 0);
  if (detail.workspaceRevision < floor) { this.publish({ ...this.state, material: { ...this.state.material, loading: false } }); this.schedule('material'); return; }
  this.publish({ ...this.state, material: { selectedId: id, detail, loading: false, error: null } });
 };
}
