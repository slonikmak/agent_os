import type { WorkspaceSnapshot as NativeSnapshot, InboxDetails as NativeInboxDetails, TaskDetails as NativeTaskDetails, MaterialAssociation as NativeMaterial, MaterialResult as NativeMaterialResult } from '../../bindings/personaldesk/internal/adapters/wailsread/models';
import type { MaterialResult, TaskDetails, MaterialAssociation, InboxDetails, InboxMessage, CallerRole, ArtifactCoordinate, BoardStatus, InboxMetadata, MessageKind, MessageStatus, SourceHealth, SourceStatus, TaskKind, TaskSummary, WorkspaceSnapshot } from './models.ts';

function kind(value: string): value is TaskKind { return ['task_analysis', 'code_review', 'request'].includes(value); }
function status(value: string): value is BoardStatus { return ['new', 'preparing', 'executing', 'review', 'waiting', 'done'].includes(value); }
function messageKind(value: string): value is MessageKind { return ['user_request', 'external_signal', 'agent_report'].includes(value); }
function messageStatus(value: string): value is MessageStatus { return ['pending', 'needs_attention', 'applied', 'ignored'].includes(value); }
function sourceStatus(value: string): value is SourceStatus { return ['ready', 'error', 'unavailable', 'not_configured'].includes(value); }
function role(value: string): value is CallerRole { return ['reader', 'worker', 'coordinator'].includes(value); }
function mapMaterial(artifact: NativeMaterial): MaterialAssociation {
 if (!['file','url','reference'].includes(artifact.coordinate.kind) || !['available','unavailable'].includes(artifact.availability) ||
     (artifact.unavailableReason !== null && !['not_imported','missing','unsupported','too_large'].includes(artifact.unavailableReason))) throw new Error('Unsupported material response');
 return { coordinate: { ...artifact.coordinate, kind: artifact.coordinate.kind as ArtifactCoordinate['kind'] }, materialId: artifact.materialId,
  availability: artifact.availability as MaterialAssociation['availability'], unavailableReason: artifact.unavailableReason as MaterialAssociation['unavailableReason'] };
}

export function mapTask(data: NativeTaskDetails): TaskDetails {
 const task = data.task;
 if (!task || !kind(task.kind) || !status(task.status) || !task.timeline || !task.materials ||
     (task.provenance && !role(task.provenance.submittedBy.role))) throw new Error('Unsupported task response');
 return { workspaceRevision: data.workspaceRevision, task: { ...task, kind: task.kind, status: task.status,
  provenance: task.provenance ? { ...task.provenance, submittedBy: { ...task.provenance.submittedBy, role: task.provenance.submittedBy.role as CallerRole } } : null,
  timeline: task.timeline.map(event => {
   if (event.kind !== 'task.created' || !role(event.provenance.submittedBy.role) || event.appliedBy.role !== 'coordinator') throw new Error('Unsupported task response');
   return { ...event, kind: 'task.created', provenance: { ...event.provenance, submittedBy: { ...event.provenance.submittedBy, role: event.provenance.submittedBy.role } }, appliedBy: { ...event.appliedBy, role: 'coordinator' } };
  }), materials: task.materials.map(mapMaterial) } };
}

// Explicit native-to-view mapping, with no command or HTTP authority.
export function mapWorkspace(data: NativeSnapshot): WorkspaceSnapshot {
 if (!data.tasks || !data.inbox || !data.sources) throw new Error('Unsupported workspace response');
 const tasks: TaskSummary[] = [];
 for (const task of data.tasks) {
  if (!kind(task.kind) || !status(task.status)) throw new Error('Unsupported workspace response');
  tasks.push({ ...task, kind: task.kind, status: task.status });
 }
 const inbox: InboxMetadata[] = [];
 for (const message of data.inbox) {
  if (!messageKind(message.kind) || !messageStatus(message.status)) throw new Error('Unsupported workspace response');
  inbox.push({ ...message, kind: message.kind, status: message.status });
 }
 const sources: SourceHealth[] = [];
 for (const source of data.sources) {
  if (!sourceStatus(source.status)) throw new Error('Unsupported workspace response');
  sources.push({ ...source, status: source.status });
 }
 return { ...data, tasks, inbox, sources };
}

export function mapInbox(data: NativeInboxDetails): InboxDetails {
 const message = data.message;
 if (!message || !messageKind(message.kind) || !messageStatus(message.status) || !message.artifacts ||
     !['reader','worker','coordinator'].includes(message.submittedBy.role)) throw new Error('Unsupported Inbox response');
 const artifacts: InboxMessage['artifacts'] = message.artifacts.map(artifact => {
  if (!['file','url','reference'].includes(artifact.coordinate.kind) || !['available','unavailable'].includes(artifact.availability) ||
      (artifact.unavailableReason !== null && !['not_imported','missing','unsupported','too_large'].includes(artifact.unavailableReason))) throw new Error('Unsupported Inbox response');
  return { coordinate: { ...artifact.coordinate, kind: artifact.coordinate.kind as ArtifactCoordinate['kind'] }, materialId: artifact.materialId,
   availability: artifact.availability as 'available' | 'unavailable', unavailableReason: artifact.unavailableReason as InboxMessage['artifacts'][number]['unavailableReason'] };
 });
 if (message.resolution && (message.resolution.action !== 'createTask' || message.resolution.appliedBy.role !== 'coordinator')) throw new Error('Unsupported Inbox response');
 return { workspaceRevision: data.workspaceRevision, message: { ...message, kind: message.kind, status: message.status, artifacts,
  submittedBy: { ...message.submittedBy, role: message.submittedBy.role as CallerRole },
  resolution: message.resolution ? { ...message.resolution, action: 'createTask', appliedBy: { ...message.resolution.appliedBy, role: 'coordinator' } } : null } };
}

export function mapMaterialResult(data: NativeMaterialResult): MaterialResult {
 const material = data.material;
 if (!Number.isSafeInteger(data.workspaceRevision) || data.workspaceRevision < 1 || !material || typeof material.id !== 'string') throw new Error('Unsupported material response');
 if (material.availability === 'available' && (material.mediaType === 'text/plain' || material.mediaType === 'text/markdown') && typeof material.text === 'string' && Array.from(material.text).length <= 262144 && material.unavailableReason === null) {
  return { workspaceRevision: data.workspaceRevision, material: { ...material, availability: 'available', mediaType: material.mediaType, text: material.text, unavailableReason: null } };
 }
 if (material.availability === 'unavailable' && material.mediaType === null && material.text === null && ['missing', 'unsupported', 'too_large'].includes(material.unavailableReason ?? '')) {
  return { workspaceRevision: data.workspaceRevision, material: { ...material, availability: 'unavailable', mediaType: null, text: null, unavailableReason: material.unavailableReason as 'missing' | 'unsupported' | 'too_large' } };
 }
 throw new Error('Unsupported material response');
}
