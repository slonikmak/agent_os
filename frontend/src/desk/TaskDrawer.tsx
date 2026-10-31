import { useEffect, useRef, useState } from 'react';
import type { RefObject } from 'react';
import { SafeLink, SafeText } from './SafeText';
import type { MaterialState, TaskState } from '../services/read-adapter';
import type { ArtifactCoordinate, MaterialAssociation } from '../services/models';

const tabs = [{ id: 'content', label: 'Содержание' }, { id: 'history', label: 'История' }, { id: 'sources', label: 'Источники' }] as const;
const statusNames = { new: 'Новые', preparing: 'Очередь', executing: 'Выполняется', review: 'Нужно моё внимание', waiting: 'Ждём', done: 'Завершено' };
const kindNames = { task_analysis: 'Разработка', code_review: 'Ревью', request: 'Другое' };
const coordinateNames: Record<ArtifactCoordinate['kind'], string> = { file: 'Файл', url: 'Ссылка', reference: 'Справочная координата' };
const availabilityNames: Record<MaterialAssociation['availability'], string> = { available: 'Сохранённый материал', unavailable: 'Содержимое недоступно' };
const unavailableNames: Record<NonNullable<MaterialAssociation['unavailableReason']>, string> = { not_imported: 'Не импортировано', missing: 'Не найдено', unsupported: 'Формат не поддерживается', too_large: 'Превышен размер' };
const fmt = (value: string) => new Intl.DateTimeFormat('ru-RU', { timeZone: 'Europe/Moscow', day: '2-digit', month: '2-digit', year: 'numeric', hour: '2-digit', minute: '2-digit' }).format(new Date(value)) + ' МСК';

export default function TaskDrawer({ state, materialState, selectMaterial, close, refresh, openInbox, returnFocus }: { state: TaskState; materialState: MaterialState; selectMaterial: (id: string | null) => void; close: () => void; refresh: () => Promise<void>; openInbox: (id: string) => void; returnFocus: RefObject<HTMLElement> }) {
 const dialog = useRef<HTMLElement>(null);
 const [tab, setTab] = useState<(typeof tabs)[number]['id']>('content');
 const material = materialState.detail?.material.id === materialState.selectedId ? materialState.detail.material : null;
 const task = state.detail?.task.id === state.selectedId ? state.detail.task : null;
 useEffect(() => {
  dialog.current?.focus();
  // The shell's inert state is removed in the same effect flush. Restore focus
  // after that flush, when the originating card/link can receive it again.
  return () => { queueMicrotask(() => { const trigger = returnFocus.current; if (trigger?.isConnected) trigger.focus(); }); };
 }, [returnFocus]);
 useEffect(() => { setTab('content'); }, [state.selectedId]);
 return <div className="pd-drawer-overlay" onClick={close}>
  <section className="pd-drawer" ref={dialog} tabIndex={-1} role="dialog" aria-modal="true" aria-labelledby="pd-task-title" aria-busy={state.loading} onClick={event => event.stopPropagation()} onKeyDown={event => {
   if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); close(); return; }
   if (event.key !== 'Tab') return;
   // WKWebView may skip native buttons according to macOS keyboard settings.
   // Own every step, including the interior, and respect the roving tab stop.
   event.preventDefault();
   const elements = Array.from(dialog.current?.querySelectorAll<HTMLElement>('button,a[href],input,select,textarea,summary,[tabindex]') ?? []).filter(element => element.tabIndex >= 0 && !element.matches(':disabled') && !element.closest('[hidden],[inert]') && element.getClientRects().length > 0 && getComputedStyle(element).visibility === 'visible');
   if (!elements.length) { dialog.current?.focus(); return; }
   const current = elements.indexOf(document.activeElement as HTMLElement);
   const next = current < 0 ? event.shiftKey ? elements.length - 1 : 0 : (current + (event.shiftKey ? -1 : 1) + elements.length) % elements.length;
   elements[next].focus();
  }}>
   <div className="pd-drawer-top"><span><span className="pd-saved-id">{state.selectedId}</span>{task && <span className="pd-muted">{kindNames[task.kind]}</span>}</span><button type="button" className="pd-icon-btn" onClick={close} aria-label="Закрыть задачу">✕</button></div>
   <div className="pd-drawer-body">
    {state.error && <div className="pd-read-error" role="alert"><span>{task ? 'Не удалось обновить. Показана последняя сохранённая задача.' : 'Не удалось прочитать сохранённую задачу.'}<br/><code>{state.error.code}{state.error.errorId ? ` · ${state.error.errorId}` : ''}</code></span><button tabIndex={0} onClick={() => { const error = state.error; if (error) void navigator.clipboard.writeText([error.code, error.errorId, error.requestId].filter(Boolean).join(' · ')).catch(() => {}); }}>Копировать код</button></div>}
    {task ? <><span className={`pd-pill ${task.status}`}>{statusNames[task.status]}</span><h2 id="pd-task-title">{task.title}</h2>{task.summary && <p className="pd-drawer-summary">{task.summary}</p>}<div className="pd-compact-meta"><span>Ревизия задачи {task.revision}</span><span>Обновлено: {fmt(task.updatedAt)}</span></div></> : <><h2 id="pd-task-title">Сохранённая задача</h2><p className="pd-muted" role="status">{state.loading ? 'Читаем сохранённую задачу…' : 'Данные задачи недоступны.'}</p></>}
    <div className="pd-detail-tabs" role="tablist" aria-label="Разделы задачи" onKeyDown={event => {
     const current = tabs.findIndex(item => item.id === tab);
     let next: number;
     if (event.key === 'ArrowRight') next = (current + 1) % tabs.length;
     else if (event.key === 'ArrowLeft') next = (current + tabs.length - 1) % tabs.length;
     else if (event.key === 'Home') next = 0;
     else if (event.key === 'End') next = tabs.length - 1;
     else return;
     event.preventDefault(); setTab(tabs[next].id); dialog.current?.querySelector<HTMLElement>(`#pd-task-tab-${tabs[next].id}`)?.focus();
    }}>{tabs.map(item => <button type="button" role="tab" id={`pd-task-tab-${item.id}`} aria-controls={`pd-task-panel-${item.id}`} aria-selected={tab === item.id} tabIndex={tab === item.id ? 0 : -1} className={tab === item.id ? 'active' : ''} key={item.id} onClick={() => setTab(item.id)}>{item.label}</button>)}</div>
    {tabs.map(item => <div role="tabpanel" id={`pd-task-panel-${item.id}`} aria-labelledby={`pd-task-tab-${item.id}`} hidden={tab !== item.id} tabIndex={0} key={item.id}>
     {task && item.id === 'content' && <><section className="pd-detail-section"><h3>Сохранённые сведения</h3><dl className="pd-inbox-provenance"><dt>Создано</dt><dd>{fmt(task.createdAt)}</dd><dt>Обновлено</dt><dd>{fmt(task.updatedAt)}</dd><dt>Тип</dt><dd>{kindNames[task.kind]}</dd><dt>Снимок</dt><dd>Ревизия {state.detail?.workspaceRevision}</dd></dl></section>
      {!!task.materials.filter(item => item.materialId).length && <section className="pd-detail-section"><h3>Сохранённые материалы</h3><div className="pd-material-choices">{task.materials.filter(item => item.materialId).map(item => <button className="pd-btn" key={item.materialId} aria-pressed={materialState.selectedId === item.materialId} onClick={() => selectMaterial(item.materialId)}>{item.coordinate.label ?? item.materialId}</button>)}</div></section>}
      {materialState.selectedId && <section className="pd-detail-section pd-material-reader" aria-busy={materialState.loading} aria-label="Сохранённый материал">
       <h3>Материал</h3><small className="pd-material-id">{materialState.selectedId}{materialState.detail ? ` · Снимок ${materialState.detail.workspaceRevision}` : ''}</small>
       {materialState.error && <div className="pd-read-error" role="alert"><span>{material ? 'Не удалось обновить. Показан последний сохранённый материал.' : 'Не удалось прочитать материал.'}<br/><code>{materialState.error.code}{materialState.error.errorId ? ` · ${materialState.error.errorId}` : ''}</code></span><button tabIndex={0} onClick={() => { const error = materialState.error; if (error) void navigator.clipboard.writeText([error.code, error.errorId, error.requestId].filter(Boolean).join(' · ')).catch(() => {}); }}>Копировать код</button></div>}
       {materialState.loading && <p role="status" className="pd-muted">Читаем сохранённый материал…</p>}
       {material?.availability === 'available' && <><small>{material.mediaType}</small><pre className="pd-material-text"><SafeText text={material.text ?? ''}/></pre>{material.text === '' && <p className="pd-muted">Пустой текстовый материал.</p>}</>}
       {material?.availability === 'unavailable' && <p className="pd-material-unavailable" role="status">Содержимое недоступно · {material.unavailableReason ? unavailableNames[material.unavailableReason] : 'Неизвестная причина'}. Сохранённые сведения задачи доступны.</p>}
       <button className="pd-btn" aria-disabled={materialState.loading} onClick={() => { if (!materialState.loading) void refresh(); }}>Обновить материал</button>
      </section>}
     </>}
     {task && item.id === 'history' && <section className="pd-detail-section"><h3>История · {task.timeline.length}</h3>{task.timeline.length ? <div className="pd-timeline expanded">{task.timeline.map(event => <div key={event.id}><i/><span><strong>Задача создана</strong><p className="pd-original">{event.reason}</p><small>{fmt(event.at)}</small><p>Применивший координатор: {event.appliedBy.clientId} · {event.appliedBy.role}</p><p>Исходный локальный клиент: {event.provenance.submittedBy.clientId} · {event.provenance.submittedBy.role}</p><p>Сообщение Inbox: {event.provenance.messageId}</p><small>{event.id}</small></span></div>)}</div> : <p className="pd-muted">Сохранённых событий нет.</p>}</section>}
     {task && item.id === 'sources' && <>
      <section className="pd-detail-section"><h3>Происхождение</h3>{task.provenance ? <><dl className="pd-inbox-provenance"><dt>Подавший локальный клиент</dt><dd>{task.provenance.submittedBy.clientId}</dd><dt>Роль локального клиента</dt><dd>{task.provenance.submittedBy.role}</dd></dl><button className="pd-related-message" onClick={() => openInbox(task.provenance!.messageId)}><span>Исходное сообщение Inbox</span><strong>{task.provenance.messageId}</strong><small>Открыть сохранённое сообщение</small></button><p className="pd-inline-help">Клиент подтверждён локальным ключом. Заявленные источник и отправитель показаны в исходном сообщении.</p></> : <p className="pd-muted">Происхождение не сохранено.</p>}</section>
      <section className="pd-detail-section"><h3>Координаты материалов · {task.materials.length}</h3>{task.materials.length ? <ul className="pd-artifact-paths">{task.materials.map((material, index) => <li key={index}><span>{material.coordinate.label && <strong>{material.coordinate.label}<br/></strong>}<code><SafeLink value={material.coordinate.coordinate}/></code><small>{coordinateNames[material.coordinate.kind]} · {availabilityNames[material.availability]}{material.unavailableReason ? ` · ${unavailableNames[material.unavailableReason]}` : ''}{material.materialId ? ` · ${material.materialId}` : ''}</small>{material.materialId && <button className="pd-btn" onClick={() => { selectMaterial(material.materialId); setTab('content'); dialog.current?.querySelector<HTMLElement>('#pd-task-tab-content')?.focus(); }}>Читать материал</button>}</span></li>)}</ul> : <p className="pd-muted">Сохранённых координат нет.</p>}</section>
     </>}
    </div>)}
   </div>
   <div className="pd-drawer-footer">{task?.nextAction && <span>{task.status === 'done' ? 'Итог' : 'Следующий шаг'}<br/><strong>{task.nextAction}</strong></span>}{task?.nextActor && <span>Кто: {task.nextActor}</span>}<button className="pd-btn" aria-disabled={state.loading} onClick={() => { if (!state.loading) void refresh(); }}>{state.loading ? 'Обновляем…' : 'Обновить задачу'}</button><p>Сохранённое состояние · только просмотр.</p></div>
  </section>
 </div>;
}
