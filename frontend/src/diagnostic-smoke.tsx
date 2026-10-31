// Separate entry point built only by scripts/package-diagnostic-smoke-mac.sh.
// These controls and secret canaries are absent from the production asset graph.
import { useEffect, useState, useSyncExternalStore } from 'react';
import { createRoot } from 'react-dom/client';
import { workspace, diagnostics } from './services/native';
import { RenderBoundary, RuntimeNotice, DiagnosticStatusNotice } from './desk/Diagnostics';
import PersonalDesk from './desk/PersonalDesk';
import './style.css';
function Crash(): never {throw new Error('SYNTHETIC_SECRET BUSINESS /private/diagnostic-fixture');}
function Fixture() {
 const [failed,setFailed]=useState(new URLSearchParams(window.location.search).has('diagnosticRenderFault'));
 const state=useSyncExternalStore(workspace.observe,workspace.getState);
 useEffect(()=>{workspace.start();return()=>workspace.stop();},[]);
 if(failed)return <Crash/>;
 return <PersonalDesk state={state} refresh={workspace.refresh} selectInbox={workspace.selectInbox} selectTask={workspace.selectTask} selectMaterial={workspace.selectMaterial} notices={<><div className="pd-read-error" role="group" aria-label="Diagnostic test fixture"><strong>Test fixture </strong><button onClick={()=>setFailed(true)}>Render fault</button><button onClick={()=>{setTimeout(()=>{throw new Error('SYNTHETIC_SECRET BUSINESS /private/runtime');},0);}}>Runtime fault</button><button onClick={()=>{void Promise.reject(new Error('SYNTHETIC_SECRET BUSINESS /private/promise'));}}>Promise fault</button></div><DiagnosticStatusNotice/><RuntimeNotice/></>}/>;
}
const remove=diagnostics.install(window);window.addEventListener('pagehide',()=>{remove();diagnostics.dispose();workspace.stop();},{once:true});
createRoot(document.getElementById('root')!).render(<RenderBoundary><Fixture/></RenderBoundary>);
