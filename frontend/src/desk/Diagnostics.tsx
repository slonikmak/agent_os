import React, { Component, useEffect, useState, useSyncExternalStore } from 'react';
import { diagnostics, getDiagnosticStatus } from '../services/native';
import { observeDiagnosticHealth } from '../services/diagnostic-health';
export class RenderBoundary extends Component<{ children: React.ReactNode }, { failed: boolean; errorId: string | null }> {
 state = { failed: false, errorId: null as string | null };
 private active = true;
 static getDerivedStateFromError(): { failed: boolean } { return { failed: true }; }
 componentDidCatch(): void { void diagnostics.report('ui.render_fault', 'root').then(result => { if(this.active)this.setState({errorId:result.errorId}); }); }
 componentWillUnmount(): void {this.active=false;}
 render(): React.ReactNode {
  if (this.state.failed) return <main className="pd-fallback"><h1>Personal Desk</h1><p>Не удалось отобразить рабочее пространство.</p><p>Код: ui.render_fault{this.state.errorId ? ` · ${this.state.errorId}` : ' · диагностика недоступна'}</p><button tabIndex={0} onClick={() => {void navigator.clipboard.writeText(['ui.render_fault',this.state.errorId].filter(Boolean).join(' · ')).catch(() => {});}}>Копировать код</button><button tabIndex={0} onClick={() => window.location.reload()}>Открыть заново</button></main>;
  return this.props.children;
 }
}
export function RuntimeNotice() {
 const fault=useSyncExternalStore(diagnostics.observe,diagnostics.getNotice);
 if(!fault)return null;
 return <div className="pd-read-error pd-runtime-notice" role="alert"><span>Произошла ошибка приложения. Показаны сохранённые данные.<br/><code>{fault.errorId ?? 'Диагностика недоступна'}</code></span><button tabIndex={0} onClick={()=>{void navigator.clipboard.writeText([fault.errorId,fault.requestId].filter(Boolean).join(' · ')).catch(()=>{});}}>Копировать код</button></div>;
}

// A technical status check has no domain reader/command authority and remains
// available in the failed-start shell. A broken bridge never holds the view.
export function DiagnosticStatusNotice() {
 const [health,setHealth]=useState<{degraded:boolean;restartRequired:boolean}|null>(null);
 useEffect(()=>observeDiagnosticHealth(getDiagnosticStatus,setHealth),[]);
 if(!health?.restartRequired&&!health?.degraded)return null;
 return <div className="pd-read-error pd-diagnostic-status" role="status">{health.restartRequired?'Работа остановлена после ошибки приложения. Перезапустите Personal Desk.':'Локальная диагностика ограничена. Сохранённые результаты доступны.'}</div>;
}
