import { GetDiagnosticStatus, ReportFrontendFault } from '../../bindings/personaldesk/internal/adapters/wailsread/diagnosticfacade';
import { DiagnosticReporter } from './diagnostics';
import { Events } from '@wailsio/runtime';
import { GetWorkspaceSnapshot, GetInboxMessage, GetTaskDetails, ReadMaterial } from '../../bindings/personaldesk/internal/adapters/wailsread/facade';
import { ReadAdapter } from './read-adapter';
import type { ReadResult, InboxResult, TaskResult, MaterialReadResult } from './models';
import { mapWorkspace, mapInbox, mapTask, mapMaterialResult } from './native-models';

export const getDiagnosticStatus = GetDiagnosticStatus;
export const diagnostics = new DiagnosticReporter(raw => ReportFrontendFault(raw));
export const workspace = new ReadAdapter({
 bridgeFailure: diagnostics.bridgeFailure,
 read: async (): Promise<ReadResult> => {
  const result = await GetWorkspaceSnapshot();
  if (result.error || !result.data) return { data: null, error: result.error };
  return { data: mapWorkspace(result.data), error: null };
 },
 readInbox: async (id): Promise<InboxResult> => {
  const result = await GetInboxMessage(id);
  if (result.error || !result.data) return { data: null, error: result.error };
  return { data: mapInbox(result.data), error: null };
 },
 readTask: async (id): Promise<TaskResult> => {
  const result = await GetTaskDetails(id);
  if (result.error || !result.data) return { data: null, error: result.error };
  return { data: mapTask(result.data), error: null };
 },
 readMaterial: async (id): Promise<MaterialReadResult> => {
  const result = await ReadMaterial(id);
  if (result.error || !result.data) return { data: null, error: result.error };
  return { data: mapMaterialResult(result.data), error: null };
 },
 subscribe: callback => Events.On('workspace.changed', event => {
  if (typeof event.data === 'number') callback(event.data);
 }),
});
