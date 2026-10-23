import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createContext, Script } from 'node:vm';
import test from 'node:test';
import ts from 'typescript';
const ticket = { id:'support-a', subject:'预约咨询', patientId:'patient-a', clinicId:'clinic-demo', status:'open', version:1, updatedAt:'2026-10-07T02:00:00Z' };
function harness() {
 let page, session={patient:{id:'patient-a'},token:'token-a'};
 const storage=new Map(),files=new Map(),calls=[];
 const api={supportTickets:async()=>({data:[ticket],meta:{total:1}}),supportDetail:async()=>({data:{ticket,events:[]},meta:{total:0}}),createSupport:async(body,id)=>{calls.push({body,id});return ticket;},supportAction:async(id,body,patientId)=>{calls.push({id,body,patientId});return ticket;}};
 const context=createContext({Page:d=>{page=d;page.setData=data=>Object.assign(page.data,data);},wx:{env:{USER_DATA_PATH:'/user'},getFileSystemManager:()=>({writeFileSync:(p,data)=>files.set(p,data),readFileSync:p=>files.get(p),unlinkSync:p=>files.delete(p)}),getStorageSync:k=>storage.get(k),setStorageSync:(k,v)=>storage.set(k,v),removeStorageSync:k=>storage.delete(k),stopPullDownRefresh(){}}});
 const source=ts.transpileModule(readFileSync(new URL('../app/pages/support/index.ts',import.meta.url),'utf8'),{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText;
 new Script(`(function(require,module,exports){${source}\n})`).runInContext(context)(name=>name.endsWith('/services/patient')?{patientApi:api,cachedSession:()=>session,requirePatientIdentity:id=>{if(id!==session?.patient.id)throw Error('身份已变化');return session;}}:{api:{clinics:async()=>[{id:'clinic-demo',name:'院区'}]},errorMessage:e=>e.message},{exports:{}},{});
 return {page,api,calls,storage,setSession:s=>session=s};
}
test('loads service records with Beijing time and clears previous identity data',async()=>{
 const h=harness();await h.page.load();assert.equal(h.page.data.tickets[0].timeLabel,'2026-10-07 10:00');
 h.page.data.selected=ticket;h.page.data.reply='私人回复';h.setSession(null);await h.page.load();assert.equal(h.page.data.selected,null);assert.equal(h.page.data.tickets.length,0);
});
test('lost response retains and retries the exact creation request',async()=>{
 const h=harness();await h.page.load();h.page.data.subject='咨询';h.page.data.content='希望了解预约';let attempts=0;
 h.api.createSupport=async(body,id)=>{h.calls.push({body,id});if(++attempts===1)throw Error('网络失败');return ticket;};
 await h.page.submit();assert.equal(h.page.data.pending,true);assert.equal(h.storage.size,1);
 await h.page.retry();assert.equal(h.calls.length,2);assert.equal(JSON.stringify(h.calls[0]),JSON.stringify(h.calls[1]));assert.equal(h.page.data.pending,false);assert.equal(h.page.data.selected.id,ticket.id);
});
test('reply sends current ticket version and never sends internal notes',async()=>{
 const h=harness();await h.page.load();await h.page.detail(ticket.id);h.page.data.reply='补充问题';await h.page.send();
 assert.equal(h.calls[0].body.action,'message');assert.equal(h.calls[0].body.version,1);assert.equal(h.calls[0].patientId,'patient-a');
});
test('late request from an old identity cannot display private service records',async()=>{
 const h=harness();await h.page.load();let release;h.api.supportDetail=()=>new Promise(r=>release=r);
 const pending=h.page.detail(ticket.id);h.setSession({patient:{id:'patient-b'},token:'token-b'});h.api.supportTickets=async()=>({data:[],meta:{total:0}});await h.page.load();
 release({data:{ticket,events:[{id:'private',content:'私人消息'}]},meta:{total:1}});await pending;assert.equal(h.page.data.selected,null);assert.equal(h.page.data.events.length,0);
});

test('closed ticket rating uses current version and retains an uncertain request',async()=>{
 const h=harness();await h.page.load();h.page.data.selected={...ticket,status:'closed',rating:0};h.page.data.rating=4;h.page.data.ratingComment='服务很好';let attempts=0;
 h.api.supportAction=async(id,body,patientId)=>{h.calls.push({id,body,patientId});if(++attempts===1)throw Error('连接中断');return {...ticket,status:'closed',rating:4};};
 await h.page.rate();assert.equal(h.page.data.pending,true);await h.page.retry();assert.equal(h.calls[0].body.action,'rate');assert.equal(h.calls[0].body.rating,4);assert.equal(h.calls[0].body.version,1);assert.equal(JSON.stringify(h.calls[0]),JSON.stringify(h.calls[1]));
});
test('attachment-only reply survives retry with files stored outside small key-value storage',async()=>{
 const h=harness();await h.page.load();await h.page.detail(ticket.id);h.page.data.uploads=[{name:'说明.pdf',data:'JVBERi0xLjQK'}];let attempts=0;
 h.api.supportAction=async(id,body,patientId)=>{h.calls.push({id,body,patientId});if(++attempts===1)throw Error('连接中断');return ticket;};
 await h.page.send();assert.equal(h.page.data.pending,true);assert.ok(h.storage.get('support-pending:patient-a').filePath);await h.page.retry();assert.equal(JSON.stringify(h.calls[0]),JSON.stringify(h.calls[1]));assert.equal(h.storage.size,0);assert.equal(h.page.data.uploads.length,0);
});
test('automatic message refresh preserves reply drafts and ignores hidden-page responses',async()=>{
 const h=harness();await h.page.load();await h.page.detail(ticket.id);h.page.data.reply='草稿';
 h.api.supportDetail=async()=>({data:{ticket:{...ticket,version:2},events:[{id:'new',content:'客服回复',createdAt:ticket.updatedAt}]},meta:{total:1}});
 await h.page.poll();assert.equal(h.page.data.events[0].content,'客服回复');assert.equal(h.page.data.reply,'草稿');
 let release;h.api.supportDetail=()=>new Promise(r=>release=r);const poll=h.page.poll();h.page.onHide();release({data:{ticket,events:[{id:'late',content:'迟到响应'}]},meta:{total:1}});await poll;assert.equal(h.page.data.events[0].id,'new');
});
