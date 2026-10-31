import { useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import type { ReadState } from '../services/read-adapter';
import type { BoardStatus, InboxMessage, TaskKind } from '../services/models';
import TaskDrawer from './TaskDrawer';
import { SafeLink, SafeText } from './SafeText';
import './PersonalDeskUI.css';
import './personal-desk.tokens.css';
import './personal-desk.theme.css';

const paths: Record<string, ReactNode> = {
 board: <><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M9 4v16m6-16v16"/></>,
 inbox: <><path d="m4 4-2 11v5h20v-5L20 4Z"/><path d="M2 15h6l2 3h4l2-3h6"/></>,
 link: <><path d="m10 13 4-4m-6 7-1 1a4 4 0 0 1-6-6l4-4a4 4 0 0 1 6 0m2 3 1-1a4 4 0 1 1 6 6l-4 4a4 4 0 0 1-6 0"/></>,
 refresh: <><path d="M20 7v5h-5M4 17v-5h5"/><path d="M6 6a8 8 0 0 1 13 1l1 5M4 12l1 5a8 8 0 0 0 13 1"/></>,
 focus: <><circle cx="12" cy="12" r="9"/><path d="M12 7v6m0 4h.01"/></>,
 sliders: <><path d="M3 6h5m4 0h9M3 12h11m4 0h3M3 18h3m4 0h11"/><circle cx="10" cy="6" r="2"/><circle cx="16" cy="12" r="2"/><circle cx="8" cy="18" r="2"/></>,
 message: <path d="M21 4H3v13h5l4 4 4-4h5Z"/>,
 code: <path d="m8 7-5 5 5 5m8-10 5 5-5 5m-3-12-2 20"/>,
 review: <><path d="M8 3h8l4 4v14H4V3h4m8 0v5h4M8 13h8m-8 4h5"/></>,
};
function Icon({ name, size = 18 }: { name: string; size?: number }) {
 return <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.65" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{paths[name]}</svg>;
}
const statuses: { id: BoardStatus; label: string; color: string }[] = [
 { id: 'new', label: 'Новые', color: '#9ba5ad' },
 { id: 'preparing', label: 'Очередь', color: '#a49bb4' },
 { id: 'executing', label: 'Выполняется', color: '#658bb0' },
 { id: 'review', label: 'Нужно моё внимание', color: '#b88a42' },
 { id: 'waiting', label: 'Ждём', color: '#b09a85' },
 { id: 'done', label: 'Завершено', color: '#56836c' },
];
const taskKinds: Record<TaskKind, { label: string; icon: string }> = {
 task_analysis: { label: 'Разработка', icon: 'code' },
 code_review: { label: 'Ревью', icon: 'review' },
 request: { label: 'Другое', icon: 'message' },
};
const inboxLabels: Record<string, string> = { pending: 'Ожидает разбора', needs_attention: 'Требует уточнения', applied: 'Учтено', ignored: 'Без действия' };
const filters: Record<string, string> = { unprocessed: 'Необработанные', all: 'Все сообщения', pending: 'Ожидают разбора', needs_attention: 'Требуют уточнения', applied: 'Учтены', ignored: 'Без действия' };
const fmt = (value: string | null) => value ? new Intl.DateTimeFormat('ru-RU', { timeZone: 'Europe/Moscow', day: '2-digit', month: '2-digit', year: 'numeric', hour: '2-digit', minute: '2-digit' }).format(new Date(value)) + ' МСК' : 'Ещё не было';
type View = 'desk' | 'inbox' | 'sources';

const messageKinds: Record<string, string> = { user_request: 'Запрос', external_signal: 'Внешний сигнал', agent_report: 'Отчёт агента' };
function MessageBody({ message, openTask }: { message: InboxMessage; openTask: (id: string, trigger: HTMLElement) => void }) {
 return <>
  <div className="pd-detail-meta"><span>{message.id}</span><span className={`pd-pill ${message.status}`}>{inboxLabels[message.status]}</span></div>
  <div className="pd-message-author"><span className="pd-source-icon large"><Icon name="message" size={23}/></span><span><strong>{message.sender ?? 'Отправитель не указан'}</strong><small>Заявленный источник: {message.source} · {message.scope}</small></span></div>
  <div className="pd-message-times"><span>Событие: {message.occurredAt ? fmt(message.occurredAt) : 'Не указано'}</span><span>Получено: {fmt(message.receivedAt)}</span></div>
  {message.proposedTaskRef && <p className="pd-proposed-link">Предложенная связь: {message.proposedTaskRef.taskId} · ревизия {message.proposedTaskRef.revision}</p>}
  <section className="pd-detail-section"><h3>Исходное сообщение</h3><p className="pd-original"><SafeText text={message.text}/></p></section>
  {message.sourceUrl && <section className="pd-detail-section"><h3>Координата источника</h3><p className="pd-inert-coordinate"><SafeLink value={message.sourceUrl}/></p></section>}
  {!!message.artifacts.length && <section className="pd-detail-section"><h3>Материалы для агента</h3><ul className="pd-artifact-paths">{message.artifacts.map((artifact, index) => <li key={index}><Icon name="code" size={16}/><span>{artifact.coordinate.label && <strong>{artifact.coordinate.label}<br/></strong>}<code><SafeLink value={artifact.coordinate.coordinate}/></code><small>Координата · содержимое не импортировано</small></span></li>)}</ul></section>}
  <section className="pd-detail-section"><h3>История решений · {message.resolution ? 1 : 0}</h3>{message.resolution ? <div className="pd-resolution"><small>{fmt(message.resolution.appliedAt)}</small><h4>Связано координатором: <button className="pd-task-link" tabIndex={0} onClick={event => openTask(message.resolution!.taskId, event.currentTarget)}>{message.resolution.taskId}</button></h4><p>{message.resolution.reason}</p><p>Локальный координатор: {message.resolution.appliedBy.clientId}</p></div> : <p className="pd-muted">Решение координатора ещё не сохранено. Сообщение ожидает разбора.</p>}</section>
  <details className="pd-disclosure"><summary>Координаты и происхождение</summary><dl className="pd-inbox-provenance"><dt>ID в источнике</dt><dd>{message.messageId}</dd><dt>Область источника</dt><dd>{message.scope}</dd><dt>Тип сообщения</dt><dd>{messageKinds[message.kind]}</dd><dt>Подавший локальный клиент</dt><dd>{message.submittedBy.clientId}</dd><dt>Роль локального клиента</dt><dd>{message.submittedBy.role}</dd><dt>Ревизия сообщения</dt><dd>{message.revision}</dd></dl><p className="pd-inline-help">Клиент подтверждён локальным ключом доступа. Источник и отправитель указаны в сообщении.</p></details>
 </>;
}

export default function PersonalDesk({ state, refresh, selectInbox, selectTask, selectMaterial, notices }: { notices?: ReactNode; state: ReadState; refresh: () => Promise<void>; selectInbox: (id: string | null) => void; selectTask: (id: string | null) => void; selectMaterial: (id: string | null) => void }) {
 const shell = useRef<HTMLDivElement>(null);
 const taskTrigger = useRef<HTMLElement | null>(null);
 const [view, setView] = useState<View>('desk');
 const [filter, setFilter] = useState('all');
 const [inboxFilter, setInboxFilter] = useState('unprocessed');
 const [copied, setCopied] = useState(false);
 const snapshot = state.snapshot;
 const tasks = snapshot?.tasks ?? [];
 const messages = snapshot?.inbox ?? [];
 const visible = tasks.filter(task => filter === 'all' || task.kind === filter);
 const inbox = messages.filter(message => inboxFilter === 'all' || (inboxFilter === 'unprocessed' ? ['pending', 'needs_attention'].includes(message.status) : message.status === inboxFilter));
 const selectedId = state.inbox.selectedId && (inboxFilter === 'all' || inbox.some(message => message.id === state.inbox.selectedId)) ? state.inbox.selectedId : inbox[0]?.id ?? null;
 const selectedMessage = state.inbox.detail?.message.id === selectedId ? state.inbox.detail.message : null;
 useEffect(() => { selectInbox(view === 'inbox' ? selectedId : null); }, [view, selectedId, selectInbox]);
 useEffect(() => { if (shell.current) shell.current.inert = !!state.task.selectedId; }, [state.task.selectedId]);
 const openTask = (id: string, trigger: HTMLElement) => { taskTrigger.current = trigger; selectTask(id); };
 const openRelatedInbox = (id: string) => { taskTrigger.current = null; setInboxFilter('all'); setView('inbox'); selectInbox(id); selectTask(null); requestAnimationFrame(() => { document.querySelector<HTMLElement>(`[data-message-id="${CSS.escape(id)}"]`)?.focus(); }); };
 const navigate = (next: View) => { setView(next); if (next === 'inbox') setInboxFilter('unprocessed'); };
 const title = { desk: 'Общая доска', inbox: 'Inbox', sources: 'Источники и обновление' }[view];
 const count = (value: number | undefined) => snapshot ? value : '—';
 const copyCode = async () => {
  const error = state.error;
  if (!error) return;
  try { await navigator.clipboard.writeText([error.code, error.errorId, error.requestId].filter(Boolean).join(' · ')); setCopied(true); } catch { setCopied(false); }
 };
 return <div className="pd-app pd-parity">
  <div className="pd-titlebar"><div className="pd-traffic" aria-hidden="true"/><span>Personal Desk <span className="pd-title-divider">/</span> Рабочее пространство</span><span className="pd-readonly-tag">Только просмотр</span></div>
  <div className="pd-shell" ref={shell} aria-hidden={state.task.selectedId ? true : undefined}>
   <aside className="pd-sidebar">
    <div className="pd-brand"><div className="pd-brand-symbol"><span/><span/><span/><span/></div><div><strong>Personal Desk</strong><small>PERSONAL WORKSPACE</small></div></div>
    <div className="pd-nav-label">РАБОЧЕЕ ПРОСТРАНСТВО</div>
    <nav aria-label="Главная навигация">
     <button className={view === 'desk' ? 'active' : ''} onClick={() => navigate('desk')} title="Общая доска"><Icon name="board"/><span>Общая доска</span></button>
     <button className={view === 'inbox' ? 'active' : ''} onClick={() => navigate('inbox')} title="Inbox"><Icon name="inbox"/><span>Inbox</span><b>{count(snapshot?.counts.unprocessedInbox)}</b></button>
    </nav>
    <div className="pd-nav-label second">КОНТЕКСТ</div>
    <nav><button className={view === 'sources' ? 'active' : ''} onClick={() => navigate('sources')} title="Источники и обновление"><Icon name="link"/><span>Источники и обновление</span></button></nav>
    <div className="pd-side-spacer"/>
    <div className="pd-agent-guide"><Icon name="message"/><strong>Сохранённое состояние</strong><p>Первое рабочее пространство открывается пустым.</p><small>Экран читает данные на этом устройстве.</small></div>
    <div className="pd-sidebar-bottom"><div className="pd-local"><i/> Данные на этом устройстве</div><div className="pd-user"><span className="pd-avatar">PD</span><span><strong>Личное пространство</strong><small>Personal Desk</small></span></div></div>
   </aside>
   <main className="pd-main">
    <div className="pd-page-head"><h1>{title}</h1><div className="pd-page-actions"><span className="pd-header-note">Последнее известное состояние</span><button className="pd-btn pd-sync" onClick={() => { setCopied(false); void refresh(); }} disabled={state.loading} aria-label={state.loading ? 'Обновляем сохранённые данные' : 'Обновить экран'} title="Перечитать сохранённые данные"><span className={state.loading ? 'pd-spin' : ''}><Icon name="refresh" size={15}/></span><span className="pd-refresh-label">{state.loading ? 'Обновляем…' : 'Обновить экран'}</span></button></div></div>
    {notices}
    {state.error && <div className="pd-read-error" role="alert"><span>{snapshot ? 'Не удалось обновить. Показан последний сохранённый снимок.' : 'Рабочее пространство недоступно.'}<br/><code>{state.error.code}{state.error.errorId ? ` · ${state.error.errorId}` : ''}</code></span><button tabIndex={0} onClick={() => void copyCode()}>{copied ? 'Скопировано' : 'Копировать код'}</button></div>}
    {!snapshot && <div className="pd-loading" role="status">{state.loading ? 'Читаем сохранённое рабочее пространство…' : 'Данные не загружены. Код ошибки можно скопировать выше.'}</div>}
    {view === 'desk' && <>
     <div className="pd-focus-banner"><span className="pd-focus-icon"><Icon name="focus"/></span><strong>Требуют внимания: {count(snapshot?.counts.needsAttention)}</strong><span className="pd-board-summary">Открыто: {count(snapshot ? tasks.filter(task => task.status !== 'done').length : undefined)}<span>·</span>Ждём: {count(snapshot ? tasks.filter(task => task.status === 'waiting').length : undefined)}</span></div>
     <div className="pd-toolbar"><div className="pd-filter"><Icon name="sliders" size={14}/><label htmlFor="pd-task-type">Тип задачи</label><select id="pd-task-type" value={filter} onChange={event => setFilter(event.target.value)}><option value="all">Все задачи</option>{Object.entries(taskKinds).map(([kind, item]) => <option key={kind} value={kind}>{item.label}</option>)}</select></div><span className="pd-task-count" role="status">{snapshot ? `Показано ${visible.length} из ${snapshot.counts.tasks}` : 'Ожидаем снимок'}</span></div>
     <div className="pd-board" tabIndex={0} role="region" aria-label="Колонки доски" onKeyDown={event => {
      if (event.target !== event.currentTarget) return;
      const board = event.currentTarget;
      switch (event.key) {
       case 'ArrowLeft': board.scrollBy({ left: -180 }); break;
       case 'ArrowRight': board.scrollBy({ left: 180 }); break;
       case 'Home': board.scrollTo({ left: 0 }); break;
       case 'End': board.scrollTo({ left: board.scrollWidth }); break;
       default: return;
      }
      event.preventDefault();
     }}>{statuses.map(status => <section className={`pd-column ${status.id}`} aria-label={status.label} key={status.id}><div className="pd-column-head"><i style={{ background: status.color }}/><h2>{status.label}</h2><span>{count(snapshot ? visible.filter(task => task.status === status.id).length : undefined)}</span></div><div className="pd-column-body">{visible.filter(task => task.status === status.id).map(task => <button type="button" className={`pd-task ${task.status === 'done' ? 'completed' : ''}`} key={task.id} tabIndex={0} onClick={event => openTask(task.id, event.currentTarget)} aria-label={`Открыть задачу ${task.title}`}><div className="pd-card-meta"><span><Icon name={taskKinds[task.kind].icon} size={14}/>{taskKinds[task.kind].label}</span><span title={task.id}>{task.id}</span></div><h3>{task.title}</h3>{task.summary && <p>{task.summary}</p>}<span className="pd-stage">{status.label}</span>{task.nextAction && <p>{task.nextAction}</p>}{task.nextActor && <p className="pd-muted">Кто: {task.nextActor}</p>}<div className="pd-card-foot"><time dateTime={task.updatedAt}>{fmt(task.updatedAt)}</time></div></button>)}{!visible.some(task => task.status === status.id) && <p className="pd-column-empty">{snapshot ? filter === 'all' ? 'Пока пусто' : 'Нет задач этого типа' : state.loading ? 'Загрузка…' : 'Недоступно'}</p>}</div></section>)}</div>
    </>}
    {view === 'inbox' && <>
     <div className="pd-toolbar"><div className="pd-filter"><Icon name="sliders" size={14}/><label htmlFor="pd-inbox-status">Сообщения</label><select id="pd-inbox-status" value={inboxFilter} onChange={event => setInboxFilter(event.target.value)}>{Object.entries(filters).map(([key, label]) => <option key={key} value={key}>{label}</option>)}</select></div><span className="pd-task-count" role="status">{snapshot ? `Показано ${inbox.length} из ${snapshot.counts.inbox}` : 'Ожидаем снимок'}</span><span className="pd-toolbar-hint">Просмотр не меняет статус</span></div>
     <div className="pd-inbox-layout"><div className="pd-message-list"><div className="pd-section-label">НОВЫЕ СВЕРХУ · В ПОРЯДКЕ ПОЛУЧЕНИЯ</div>{inbox.length ? inbox.map(message => <button type="button" className={`pd-message-row ${message.id === selectedId ? 'selected' : ''}`} aria-pressed={message.id === selectedId} data-message-id={message.id} key={message.id} onClick={() => selectInbox(message.id)}><span className="pd-source-icon"><Icon name="message"/></span><span className="pd-message-copy"><span className="pd-message-from">{message.sender ?? message.source}<small>{fmt(message.receivedAt)}</small></span><strong>{messageKinds[message.kind]}</strong><span className="pd-message-meta">{message.id}<span>·</span>{inboxLabels[message.status]}</span></span></button>) : <div className="pd-empty"><Icon name="inbox" size={26}/><p>{snapshot ? messages.length ? 'Сообщений с этим статусом нет.' : 'Inbox пока пуст.' : state.loading ? 'Читаем Inbox…' : 'Inbox недоступен.'}</p></div>}</div><article className="pd-message-detail" aria-label="Сохранённое сообщение" aria-busy={state.inbox.loading}>
      {state.inbox.error && state.inbox.selectedId === selectedId && <div className="pd-read-error" role="alert"><span>{selectedMessage ? 'Не удалось обновить. Показано последнее сохранённое сообщение.' : 'Не удалось прочитать исходное сообщение.'}<br/><code>{state.inbox.error.code}{state.inbox.error.errorId ? ` · ${state.inbox.error.errorId}` : ''}</code></span><button tabIndex={0} onClick={() => { const error = state.inbox.error; if (error) void navigator.clipboard.writeText([error.code, error.errorId, error.requestId].filter(Boolean).join(' · ')).catch(() => {}); }}>Копировать код</button></div>}
      {selectedMessage ? <MessageBody message={selectedMessage} openTask={openTask}/> : <div className="pd-empty" role="status"><p>{selectedId ? state.inbox.loading ? 'Читаем сохранённое сообщение…' : 'Исходное сообщение недоступно.' : snapshot ? messages.length ? 'Выберите другой фильтр сообщений.' : 'Здесь появятся сохранённые сообщения.' : 'Ожидаем сохранённое состояние.'}</p></div>}
     </article></div>
    </>}
    {view === 'sources' && <div className="pd-source-page"><p className="pd-inline-help">Здесь показаны сохранённые сведения, сообщённые агентами. Обновление экрана перечитывает локальный снимок.</p><div className="pd-source-list">{snapshot?.sources.length ? snapshot.sources.map(source => <section className="pd-source-reading" key={source.source + source.scope}><div className="pd-source-heading"><span className="pd-source-icon"><Icon name="link"/></span><div><h2>{source.source}</h2><p>{source.scope}</p></div><span className={`pd-source-health ${source.status}`}>{source.status}</span></div><div className="pd-source-dates"><span>Успешное чтение, по сообщению агента<br/><strong>{fmt(source.lastSuccessAt)}</strong></span><span>Последняя попытка, по сообщению агента<br/><strong>{fmt(source.lastAttemptAt)}</strong></span></div></section>) : <div className="pd-empty"><Icon name="link" size={26}/><p>{snapshot ? 'Нет сохранённых сведений об источниках от агентов.' : 'Сведения об источниках не загружены.'}</p></div>}</div></div>}
    <footer className="pd-footer"><span>{snapshot ? `Сохранённый снимок · ревизия ${snapshot.workspaceRevision}` : 'Снимок не загружен'}</span><span>Только просмотр</span></footer>
   </main>
  </div>
  {state.task.selectedId && <TaskDrawer state={state.task} materialState={state.material} selectMaterial={selectMaterial} close={() => selectTask(null)} refresh={refresh} openInbox={openRelatedInbox} returnFocus={taskTrigger}/>}
 </div>;
}
