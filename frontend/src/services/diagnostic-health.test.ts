import test from 'node:test';
import assert from 'node:assert/strict';
import { observeDiagnosticHealth } from './diagnostic-health.ts';
import type { DiagnosticHealth, HealthRequest } from './diagnostic-health.ts';
import { ReadAdapter } from './read-adapter.ts';
import type { ReadResult } from './models.ts';
const settle = async () => { for(let i=0;i<5;i++)await Promise.resolve(); };

test('technical health changes to degraded while business reads remain successful', async t => {
 t.mock.timers.enable({apis:['setTimeout']});
 let degraded=false,calls=0;
 const health:DiagnosticHealth[]=[];
 const stop=observeDiagnosticHealth(async()=>{calls++;return {degraded,restartRequired:false};},value=>health.push(value));
 const saved:ReadResult={data:{workspaceId:'saved',workspaceRevision:1,createdAt:'2026-10-29T00:00:00Z',tasks:[],inbox:[],sources:[],counts:{tasks:0,inbox:0,unprocessedInbox:0,needsAttention:0}},error:null};
 const business=new ReadAdapter({read:async()=>saved,subscribe:()=>()=>{}});business.start();await settle();
 assert.deepEqual(health,[{degraded:false,restartRequired:false}]);assert.equal(business.getState().error,null);
 degraded=true;await business.refresh();t.mock.timers.tick(2000);await settle();
 assert.deepEqual(health.at(-1),{degraded:true,restartRequired:false});assert.equal(calls,2);
 assert.equal(business.getState().error,null);assert.equal(business.getState().snapshot,saved.data);
 stop();business.stop();t.mock.timers.tick(10_000);await settle();assert.equal(calls,2);
});

test('hung status calls are cancelled without overlapping requests and stop ignores late results', async t=>{
 t.mock.timers.enable({apis:['setTimeout']});
 let resolve!:(health:DiagnosticHealth)=>void;let calls=0,cancels=0,changes=0;
 const pending=Object.assign(new Promise<DiagnosticHealth>(done=>{resolve=done;}),{cancel:()=>{cancels++;}}) as HealthRequest;
 const stop=observeDiagnosticHealth(()=>{calls++;return pending;},()=>{changes++;});
 t.mock.timers.tick(1000);assert.equal(cancels,1);t.mock.timers.tick(60_000);await settle();assert.equal(calls,1);
 stop();resolve({degraded:true,restartRequired:true});await settle();t.mock.timers.tick(60_000);await settle();
 assert.equal(changes,0);assert.equal(calls,1);
});

test('rejected status reads retry at a bounded interval and teardown cancels the next timer', async t=>{
 t.mock.timers.enable({apis:['setTimeout']});let calls=0;
 const stop=observeDiagnosticHealth(async()=>{calls++;throw new Error('PRIVATE bridge');},()=>{assert.fail('rejection became a health value');});
 await settle();assert.equal(calls,1);t.mock.timers.tick(1999);await settle();assert.equal(calls,1);
 t.mock.timers.tick(1);await settle();assert.equal(calls,2);stop();t.mock.timers.tick(10_000);await settle();assert.equal(calls,2);
});
