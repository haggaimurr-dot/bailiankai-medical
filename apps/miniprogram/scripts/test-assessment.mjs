import { readFileSync } from 'node:fs';
import { createContext, Script } from 'node:vm';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import assert from 'node:assert/strict';
import test from 'node:test';
import ts from 'typescript';
const root=resolve(dirname(fileURLToPath(import.meta.url)),'../app');
const patient=id=>({token:`token-${id}`,patient:{id,displayName:'工程患者',version:1,loginKind:'demo'},expiresAt:'2099-01-01T00:00:00Z'});
const task=(status='draft',publishedReview=null)=>({task:{id:'assessment-test',patientId:'patient-a',clinicId:'clinic-demo',appointmentId:'',supersedesAssessmentId:'',version:3,status,attemptCount:0,maxAttempts:3,failureCode:'',retryable:false,createdAt:'2026-10-10T09:00:00+08:00',updatedAt:'2026-10-10T09:00:00+08:00',nextAttemptAt:null,submittedAt:null,analysisCompletedAt:null,reviewedAt:null},photos:[],consent:{version:'photo-ai-v1',content:'工程AI授权文本',granted:true},publishedReview});
function harness(name,logged=true){
 let page,sequence=0;const requests=[],uploads=[],modals=[],navigation=[],timers=new Map(),storage=new Map();
 storage.set('bailiankai.patient-session',logged?patient('patient-a'):null);
 const context=createContext({Page(def){page=def;page.setData=d=>Object.assign(page.data,d);},setTimeout:fn=>{const id=++sequence;timers.set(id,fn);return id;},clearTimeout:id=>timers.delete(id),wx:{request:o=>requests.push(o),uploadFile:o=>uploads.push(o),getStorageSync:k=>storage.get(k),setStorageSync:(k,v)=>storage.set(k,v),removeStorageSync:k=>storage.delete(k),showModal:o=>modals.push(o),navigateTo:o=>navigation.push(o.url),stopPullDownRefresh(){},getFileSystemManager:()=>({unlink(){}}),previewImage(){}}});
 const cache=new Map();function load(file){if(cache.has(file))return cache.get(file).exports;const module={exports:{}};cache.set(file,module);const source=ts.transpileModule(readFileSync(file,'utf8'),{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText;new Script(`(function(require,module,exports){${source}\n})`).runInContext(context)(id=>load(resolve(dirname(file),`${id}.ts`)),module,module.exports);return module.exports;}
 load(resolve(root,`pages/${name}/index.ts`));return{get page(){return page},requests,uploads,modals,navigation,timers,storage,sessionKey:load(resolve(root,'config/storage.ts')).SESSION_STORAGE_KEY};
}
const tick=()=>new Promise(setImmediate);
function respond(r,data,meta){assert.ok(r,'missing request');r.success({statusCode:200,data:{data,...(meta?{meta}:{})}});}
function login(h,id='patient-a'){h.storage.set(h.sessionKey,patient(id));}

test('assessment requires a patient identity and has no anonymous demo or physician actions',async()=>{
 const h=harness('assessment',false);h.page.onShow();await tick();assert.equal(h.page.data.needsLogin,true);assert.equal(h.requests.length,0);h.page.login();assert.equal(h.navigation[0],'/pages/profile/index');
 const markup=readFileSync(resolve(root,'pages/assessment-tasks/index.wxml'),'utf8');assert.ok(!markup.includes('模拟医生'));assert.ok(!markup.includes('重置本机'));
});
test('creation response loss restores the same patient-scoped creation request',async()=>{
 const h=harness('assessment');login(h);h.page.visible=true;h.page.patientId='patient-a';h.page.data.clinics=[{id:'clinic-demo',name:'工程院区'}];
 const first=h.page.create();await tick();assert.equal(h.requests[0].header.Authorization,'Bearer token-patient-a');const body=h.requests[0].data;h.requests[0].fail();await first;assert.ok(h.page.data.recovering);
 const retry=h.page.create();await tick();assert.deepEqual(h.requests[1].data,body);respond(h.requests[1],task());await retry;assert.equal(h.page.id,'assessment-test');assert.equal(h.page.data.recovering,false);h.page.onHide();assert.equal(h.timers.size,0);
});
test('pending patient operation survives response loss and reuses body and idempotency key',async()=>{
 const h=harness('assessment-tasks');login(h);h.page.visible=true;h.page.patientId='patient-a';h.page.id='assessment-test';h.page.data.selected=task('failed');
 const first=h.page.perform('retry');await tick();const request=h.requests[0];request.fail();await first;assert.equal(h.page.data.pending,true);
 // A refresh starts independently; finish it before retrying the preserved action.
 await tick();if(h.requests[1])respond(h.requests[1],task('failed'));await tick();
 const again=h.page.perform();await tick();const retried=h.requests.findLast(r=>r.url.endsWith('/retry'));assert.deepEqual(retried.data,request.data);assert.equal(retried.header['Idempotency-Key'],request.header['Idempotency-Key']);respond(retried,task('queued'));await again;h.page.onHide();assert.equal(h.page.data.selected,null);assert.equal(h.timers.size,0);
});
test('patient result is server-projected, withdrawal and missing ownership clear visible content',async()=>{
 const h=harness('assessment-result');login(h);h.page.visible=true;h.page.id='assessment-test';let read=h.page.load();await tick();respond(h.requests[0],task('awaiting_review'));await read;assert.equal(h.page.data.review,null);
 const review={id:'review-1',version:1,decision:'approved',note:'工程复核',doctorName:'测试医生',reviewedAt:'2026-10-10T09:05:00+08:00',finalContent:{summary:'医生发布内容',observations:[],limitations:['工程测试'],consultationAdvice:{recommended:true,reason:'测试建议',preparation:[]}}};
 read=h.page.load();await tick();respond(h.requests[1],task('completed',review));await read;assert.equal(h.page.data.review.finalContent.summary,'医生发布内容');
 read=h.page.load();assert.equal(h.page.data.review,null);await tick();respond(h.requests[2],task('cancelled'));await read;assert.equal(h.page.data.review,null);assert.equal(h.page.data.label,'已取消');
 login(h,'patient-b');read=h.page.load();await tick();assert.equal(h.requests[3].header.Authorization,'Bearer token-patient-b');h.requests[3].success({statusCode:404,data:{error:{code:'ASSESSMENT_NOT_FOUND',message:'任务不存在'}}});await read;assert.equal(h.page.data.review,null);assert.equal(h.page.data.label,'');h.page.onHide();assert.equal(h.timers.size,0);
});
test('late responses after hiding cannot restore patient data; a new identity loads its own queue',async()=>{
 const h=harness('assessment-tasks');login(h);h.page.visible=true;h.page.id='assessment-test';const read=h.page.load();await tick();h.page.onHide();respond(h.requests[0],task('completed'));await read;assert.equal(h.page.data.selected,null);assert.equal(h.timers.size,0);
 login(h,'patient-b');h.page.visible=true;const next=h.page.load();await tick();assert.equal(h.page.id,'');assert.equal(h.requests[1].header.Authorization,'Bearer token-patient-b');respond(h.requests[1],[],{page:1,pageSize:20,total:0});await next;assert.equal(h.page.data.tasks.length,0);h.page.onHide();
});
test('photo upload keeps original file and request key on network error and clears on hiding',async()=>{
 const h=harness('assessment');login(h);h.page.visible=true;h.page.id='assessment-test';h.page.patientId='patient-a';h.page.data.view=task();h.page.pendingUpload={angle:'front',filePath:'/tmp/synthetic.jpg',version:3,key:'stable-upload'};
 const first=h.page.upload();await tick();respond(h.requests[0],{accepted:true});await tick();const original=h.uploads[0];assert.equal(original.formData.version,'3');original.fail();await first;assert.equal(h.page.data.uploadPending,true);
 const retry=h.page.upload();await tick();respond(h.requests[1],{accepted:true});await tick();assert.equal(h.uploads[1].filePath,original.filePath);assert.equal(h.uploads[1].header['Idempotency-Key'],original.header['Idempotency-Key']);h.uploads[1].success({statusCode:200,data:JSON.stringify({data:task()})});await retry;assert.equal(h.page.pendingUpload,null);h.page.onHide();assert.equal(h.page.data.view,null);
});
test('rejected review displays explanation without observations and offers linked retake',async()=>{
 const h=harness('assessment-result');login(h);h.page.visible=true;h.page.id='assessment-test';const read=h.page.load();await tick();respond(h.requests[0],task('rejected',{id:'review-1',version:1,decision:'rejected',note:'请补拍',doctorName:'测试医生',reviewedAt:'2026-10-10T09:05:00+08:00',finalContent:null}));await read;assert.equal(h.page.data.review.finalContent,null);h.page.retake();assert.match(h.navigation[0],/supersedes=assessment-test/);h.page.onHide();
});
