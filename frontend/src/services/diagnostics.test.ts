import test from 'node:test';
import assert from 'node:assert/strict';
import { DiagnosticReporter } from './diagnostics.ts';
const id='a'.repeat(32), requestId='b'.repeat(32);
test('fixed frontend reports deduplicate and never inspect thrown values', async () => {
 const calls: string[]=[];let now=0;
 const reporter=new DiagnosticReporter(async raw=>{calls.push(raw);return {status:'accepted',requestId,errorId:id};},()=>now);
 const results=await Promise.all([reporter.report('ui.render_fault','root'),reporter.report('ui.render_fault','root')]);
 assert.equal(calls.length,1);assert.equal(results[0].errorId,id);
 assert.deepEqual(JSON.parse(calls[0]),{schemaVersion:1,code:'ui.render_fault',component:'root',frames:[]});
 now=60_000;await reporter.report('ui.render_fault','root');assert.equal(calls.length,2);
});
test('rejection, malformed response and a hung bridge settle safely without another report', async () => {
 for(const send of [async()=>{throw new Error('SECRET /private')},async()=>({status:'accepted',errorId:'SECRET',requestId}),()=>new Promise<never>(()=>{})]) {
  const reporter=new DiagnosticReporter(send,Date.now,5);
  const result=await reporter.bridgeFailure();assert.equal(result.errorId,null);assert.equal(result.code,'service_unavailable');assert.equal(result.requestId,'');
  assert.equal(JSON.stringify(reporter.getNotice()).includes('SECRET'),false);
 }
});
test('global hooks use fixed metadata and teardown removes both listeners', async () => {
 const target=new EventTarget();let calls=0;
 const reporter=new DiagnosticReporter(async()=>{calls++;return {status:'accepted',requestId,errorId:id}});
 const remove=reporter.install(target as unknown as Window);
 target.dispatchEvent(new Event('error'));target.dispatchEvent(new Event('error'));target.dispatchEvent(new Event('unhandledrejection'));
 await new Promise(resolve=>setTimeout(resolve,0));assert.equal(calls,2);
 remove();target.dispatchEvent(new Event('error'));await new Promise(resolve=>setTimeout(resolve,0));assert.equal(calls,2);
});
test('hung reports have no retry queue and disposal settles active reports', async () => {
 let calls=0,now=0;
 const reporter=new DiagnosticReporter(()=>{calls++;return new Promise<never>(()=>{});},()=>now,2);
 for(const component of ['root','desk','board','inbox','task_drawer','read_adapter'] as const) {await reporter.report('ui.runtime_fault',component);}
 assert.equal(calls,4);now=60_000;await reporter.report('ui.promise_fault','root');assert.equal(calls,4);
 const other=new DiagnosticReporter(()=>new Promise<never>(()=>{}),Date.now,60_000);
 const pending=other.report('ui.render_fault','root');other.dispose();assert.equal((await pending).status,'unavailable');
});
