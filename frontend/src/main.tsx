import { useEffect, useSyncExternalStore } from 'react';
import { createRoot } from 'react-dom/client';
import { workspace, diagnostics } from './services/native';
import PersonalDesk from './desk/PersonalDesk';
import './style.css';
import { RenderBoundary, RuntimeNotice, DiagnosticStatusNotice } from './desk/Diagnostics';
function App() {
 const state = useSyncExternalStore(workspace.observe, workspace.getState);
 useEffect(() => { workspace.start(); return () => workspace.stop(); }, []);
 return <PersonalDesk state={state} refresh={workspace.refresh} selectInbox={workspace.selectInbox} selectTask={workspace.selectTask} selectMaterial={workspace.selectMaterial} notices={<><DiagnosticStatusNotice/><RuntimeNotice/></>} />;
}
const removeHooks=diagnostics.install(window);
window.addEventListener('pagehide',()=>{removeHooks();diagnostics.dispose();workspace.stop();},{once:true});
createRoot(document.getElementById('root')!).render(<RenderBoundary><App /></RenderBoundary>);
