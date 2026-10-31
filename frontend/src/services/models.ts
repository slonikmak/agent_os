// Frozen E1 read inventory. Workspace, task and Inbox queries are bound.
export type TaskKind = 'task_analysis' | 'code_review' | 'request';
export type BoardCategory = 'development' | 'review' | 'other';
export type BoardStatus = 'new' | 'preparing' | 'executing' | 'review' | 'waiting' | 'done';
export type MessageKind = 'user_request' | 'external_signal' | 'agent_report';
export type MessageStatus = 'pending' | 'needs_attention' | 'applied' | 'ignored';
export type SourceStatus = 'ready' | 'error' | 'unavailable' | 'not_configured';
export type CallerRole = 'reader' | 'worker' | 'coordinator';
export const categoryForKind: Record<TaskKind, BoardCategory> = { task_analysis: 'development', code_review: 'review', request: 'other' };
export interface TaskSummary {
 id: string; kind: TaskKind; status: BoardStatus; title: string;
 summary: string | null; nextAction: string | null; nextActor: string | null;
 revision: number; updatedAt: string;
}
export interface InboxMetadata {
 id: string; source: string; scope: string; kind: MessageKind; status: MessageStatus;
 sender: string | null; occurredAt: string | null; receivedAt: string; revision: number;
}
export interface SourceHealth {
 source: string; scope: string; status: SourceStatus;
 lastAttemptAt: string | null; lastSuccessAt: string | null;
}
export interface WorkspaceSnapshot {
 workspaceId: string; workspaceRevision: number; createdAt: string;
 tasks: TaskSummary[]; inbox: InboxMetadata[]; sources: SourceHealth[];
 counts: { tasks: number; inbox: number; unprocessedInbox: number; needsAttention: number };
}
export interface Principal { clientId: string; role: CallerRole }
export interface TaskRef { taskId: string; revision: number }
export interface ArtifactCoordinate { kind: 'file' | 'url' | 'reference'; coordinate: string; label: string | null }
export interface MaterialAssociation {
 coordinate: ArtifactCoordinate; materialId: string | null; availability: 'available' | 'unavailable';
 unavailableReason: 'not_imported' | 'missing' | 'unsupported' | 'too_large' | null;
}
export interface Provenance { messageId: string; submittedBy: Principal }
export interface Resolution { id: string; action: 'createTask'; reason: string; taskId: string; appliedBy: Principal; appliedAt: string }
export interface InboxMessage extends InboxMetadata {
 messageId: string; text: string; sourceUrl: string | null; artifacts: MaterialAssociation[];
 submittedBy: Principal; proposedTaskRef: TaskRef | null; resolution: Resolution | null;
}
export interface TaskEvent { id: string; kind: 'task.created'; at: string; reason: string; provenance: Provenance; appliedBy: Principal }
export interface Task extends TaskSummary { createdAt: string; provenance: Provenance | null; timeline: TaskEvent[]; materials: MaterialAssociation[] }
export interface TaskDetails { workspaceRevision: number; task: Task }
export interface InboxDetails { workspaceRevision: number; message: InboxMessage }
export interface MaterialResult {
 workspaceRevision: number;
 material: { id: string; availability: 'available' | 'unavailable'; mediaType: 'text/plain' | 'text/markdown' | null; text: string | null; unavailableReason: 'missing' | 'unsupported' | 'too_large' | null };
}
export interface ReadFailure {
 code: string; message: string; retryable: boolean; requestId: string; errorId: string | null; currentRevision: number | null;
}
export interface ReadResult { data: WorkspaceSnapshot | null; error: ReadFailure | null }
export type FrameLocation = 'bootstrap.start' | 'queries.workspace' | 'httpapi.serve' | 'wailsread.workspace' | 'sqlite.open' | 'sqlite.workspace' | 'frontend.render' | 'frontend.runtime' | 'frontend.promise';
export interface Frame { location: FrameLocation; line: number }
export interface FrontendFrame { location: 'frontend.render' | 'frontend.runtime' | 'frontend.promise'; line: number }
export interface FrontendFault {
 schemaVersion: 1; code: 'ui.render_fault' | 'ui.runtime_fault' | 'ui.promise_fault';
 component: 'desk' | 'board' | 'inbox' | 'task_drawer' | 'read_adapter' | 'root'; frames: FrontendFrame[];
}
export interface DiagnosticCoverage { complete: boolean; acceptedRecords: number; omittedRecords: number; unavailableFiles: number; truncatedRecords: number }
export interface DiagnosticExportResult { status: 'exported' | 'cancelled' | 'failed'; coverage: DiagnosticCoverage | null; errorId: string | null }

export interface InboxResult { data: InboxDetails | null; error: ReadFailure | null }
export interface TaskResult { data: TaskDetails | null; error: ReadFailure | null }

export interface MaterialReadResult { data: MaterialResult | null; error: ReadFailure | null }
