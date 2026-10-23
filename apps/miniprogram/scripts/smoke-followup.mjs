import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { fileURLToPath } from 'node:url';

// Node 22+, live local API, and the WeChat CLI `auto` WebSocket are required.
// Uses an isolated demo identity and synthetic photos only. Restores existing
// storage and releases test capacity while retaining demo records and audit.
const endpoint = process.env.MINIPROGRAM_WS || 'ws://127.0.0.1:9420';
const apiBase = 'http://127.0.0.1:8080/api/v1';
const keys = {
  session: 'blk_dev_patient_session_v1',
  expired: 'blk_dev_patient_expired_v1',
  submission: 'blk_appointment_submission_v1',
};
const socket = new WebSocket(endpoint);
const pending = new Map();
const isolationMarker = randomUUID();
const exceptions = [];
let sequence = 0;
let connected = false;
let storageIsolated = false;
let requestMocked = false;
let modalMocked = false;
let session;
let adminToken;

socket.addEventListener('message', ({ data }) => {
  const message = JSON.parse(data);
  if (message.method === 'App.exceptionThrown') exceptions.push(message.params);
  const callback = pending.get(message.id);
  if (!callback) return;
  pending.delete(message.id);
  clearTimeout(callback.timer);
  if (message.error) callback.reject(new Error(`${callback.method} failed: ${JSON.stringify(message.error)}`));
  else callback.resolve(message.result);
});

function send(method, params = {}) {
  return new Promise((resolve, reject) => {
    if (socket.readyState !== WebSocket.OPEN) return reject(new Error('Simulator connection is not open'));
    const id = String(++sequence);
    const timer = setTimeout(() => {
      pending.delete(id);
      reject(new Error(`${method} timed out after 20 seconds`));
    }, 20000);
    pending.set(id, { method, resolve, reject, timer });
    socket.send(JSON.stringify({ id, method, params }));
  });
}

async function evaluate(fn, ...args) {
  return (await send('App.callFunction', { functionDeclaration: fn.toString(), args })).result;
}

async function waitUntil(check, label) {
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    const result = await check();
    if (result) return result;
    await new Promise((resolve) => setTimeout(resolve, 150));
  }
  throw new Error(`Timed out: ${label}`);
}

async function currentPage(path) {
  await waitUntil(async () => {
    const page = await send('App.getCurrentPage');
    return page.path === path ? page : null;
  }, path).catch(async (error) => {
    const routes = await evaluate(function pageDiagnostics() {
      return getCurrentPages().map((page) => ({ route: page.route, loading: page.data.loading, hasError: Boolean(page.data.error), slotCount: page.data.slots?.length }));
    });
    throw new Error(`${error.message}; page stack: ${JSON.stringify(routes)}`);
  });
  // Match miniprogram-automator's changeRoute: the logic-layer route can be
  // visible before the native transition completes. Its SDK waits 3 seconds.
  await new Promise((resolve) => setTimeout(resolve, 3000));
  // reLaunch may recreate the same path with a different pageId. Re-read after
  // the transition; returning the earlier handle invokes a destroyed page.
  return waitUntil(async () => {
    const page = await send('App.getCurrentPage');
    return page.path === path ? page : null;
  }, `settled ${path}`);
}

async function pageData(pageId) {
  return (await send('Page.getData', { pageId })).data;
}

async function readyData(pageId) {
  return waitUntil(async () => {
    const data = await pageData(pageId);
    if (data.error || data.initialError || data.expired) throw new Error('Simulator page reported a load or session error');
    return !data.loading && !data.initializing ? data : null;
  }, 'page API data');
}

async function call(page, method, ...args) {
  return send('Page.callMethod', { pageId: page.pageId, method, args });
}

async function renderedElements(page, selector, count) {
  return waitUntil(async () => {
    const { elements } = await send('Page.getElements', { pageId: page.pageId, selector });
    if (!elements.length || (count !== undefined && elements.length !== count)) return null;
    for (const element of elements) {
      const { properties } = await send('Element.getDOMProperties', {
        pageId: page.pageId,
        elementId: element.elementId,
        names: ['offsetWidth', 'offsetHeight'],
      });
      if (!(Number(properties[0]) > 0 && Number(properties[1]) > 0)) return null;
    }
    return elements;
  }, `rendered ${selector}`);
}

async function renderedText(page, selector, expected) {
  return waitUntil(async () => {
    const elements = await renderedElements(page, selector, 1);
    const { properties } = await send('Element.getDOMProperties', {
      pageId: page.pageId,
      elementId: elements[0].elementId,
      names: ['innerText'],
    });
    return String(properties[0] || '').trim() === expected;
  }, `rendered ${selector} status ${expected}`);
}

async function api(path, { token, method = 'GET', body } = {}) {
  const response = await fetch(`${apiBase}${path}`, {
    method,
    headers: { ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}), ...(token ? { Authorization: `Bearer ${token}` } : {}) },
    ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
    signal: AbortSignal.timeout(15000),
  });
  const envelope = await response.json();
  if (!response.ok) {
    const error = new Error(`API HTTP ${response.status}: ${envelope.error?.code || 'REQUEST_FAILED'}`);
    error.status = response.status;
    throw error;
  }
  return envelope;
}



let appointment;
let chooseMocked = false;
try {
 await new Promise((resolve,reject)=>{
  const timer=setTimeout(()=>reject(new Error('Cannot connect to simulator')),10000);
  socket.addEventListener('open',()=>{clearTimeout(timer);connected=true;resolve();},{once:true});
  socket.addEventListener('error',()=>{clearTimeout(timer);reject(new Error('Cannot connect to simulator'));},{once:true});
 });
 process.loadEnvFile(fileURLToPath(new URL('../../../.env',import.meta.url)));adminToken=process.env.DEMO_ADMIN_TOKEN;
 session=(await api('/dev/patient-sessions',{method:'POST',body:{}})).data;
 await evaluate(function isolate(storageKeys,testSession,marker){
  const app=getApp();if(app.__blkM3Smoke)throw new Error('Previous M3 smoke requires cleanup');
  const present=wx.getStorageInfoSync().keys;
  app.__blkM3Smoke={marker,backups:Object.values(storageKeys).map(key=>({key,present:present.indexOf(key)>=0,value:wx.getStorageSync(key)}))};
  wx.setStorageSync(storageKeys.session,testSession);wx.removeStorageSync(storageKeys.expired);wx.removeStorageSync(storageKeys.submission);return true;
 },keys,session,isolationMarker);storageIsolated=true;
 const slots=(await api('/slots')).data;const slot=slots.find(x=>x.status==='open' && x.reserved<x.capacity && Date.parse(x.startsAt)>Date.now()+3600000);assert.ok(slot);
 const create=await fetch(apiBase+'/me/appointments',{method:'POST',headers:{Authorization:`Bearer ${session.token}`,'Content-Type':'application/json','Idempotency-Key':randomUUID()},body:JSON.stringify({slotId:slot.id,contactName:'M3原生随访验收',contactPhone:'13800000000'})});
 assert.equal(create.status,201);appointment=(await create.json()).data;
 appointment=(await api(`/admin/appointments/${appointment.id}/review`,{token:adminToken,method:'POST',body:{version:appointment.version,action:'confirm'}})).data;
 const content={summary:'M3虚构已发布方案',stages:[{title:'阶段',body:'仅用于验收'}],instructions:'虚构内容'};
 const draft=(await api(`/admin/appointments/${appointment.id}/care-plan/draft`,{token:adminToken,method:'PUT',body:{version:0,content}})).data;
 const plan=(await api(`/admin/appointments/${appointment.id}/care-plan/publish`,{token:adminToken,method:'POST',body:{version:draft.draftVersion,changeReason:'验收发布'}})).data;
 const recorded=(await api(`/admin/appointments/${appointment.id}/treatment`,{token:adminToken,method:'POST',body:{version:appointment.version,planVersionId:plan.id,summary:'M3虚构实际治疗',notes:'原生联调',treatedAt:new Date(Date.now()-48*3600000).toISOString(),offsets:[1,3,7]}})).data;
 assert.equal(recorded.tasks.length,3);
 const latest=(await api(`/me/appointments/${appointment.id}`,{token:session.token})).data;assert.equal(latest.status,'completed');assert.equal(latest.canCancel,false);
 assert.equal((await api('/slots')).data.find(x=>x.id===slot.id).reserved,slot.reserved,'Recording treatment releases reservation');
 await evaluate(function route(url){wx.reLaunch({url});return true;},`/pages/followups/index?id=${appointment.id}`);
 const page=await currentPage('pages/followups/index');let data=await readyData(page.pageId);
 assert.equal(data.tasks.length,3);assert.equal(data.treatment.planVersionId,plan.id);assert.equal(data.active.id,recorded.tasks[0].id);
 await renderedElements(page,'.task-card',3);await renderedElements(page,'.symptoms-input',1);
 await send('App.mockWxMethod',{method:'showModal',result:{confirm:true,cancel:false}});modalMocked=true;
 await call(page,'grant');await waitUntil(async()=> (await pageData(page.pageId)).active.photosGranted,'Followup consent');
 const fixture=await evaluate(function fixture(){const path=wx.env.USER_DATA_PATH+'/m3-synthetic.png';wx.getFileSystemManager().writeFileSync(path,'iVBORw0KGgoAAAANSUhEUgAAADAAAAAwCAIAAADYYG7QAAAAQklEQVR4nO3OQQ0AIAwAsUlEDkrQiwuOR5MK6Ox1vjL5QEhISKgeCAkJCdUDISEhoXogJCQkVA+EhISE6oGQkNBjFyqzTrXicFVgAAAAAElFTkSuQmCC','base64');return path;});
 await send('App.mockWxMethod',{method:'chooseImage',result:{tempFilePaths:[fixture],tempFiles:[{path:fixture,size:120}]}});chooseMocked=true;
 await call(page,'choose',{currentTarget:{dataset:{angle:'front'}}});
 await waitUntil(async()=>{const d=await pageData(page.pageId);if(d.error)throw new Error(d.error);return !d.uploading && d.photos[0].id && d.photos[0].path;},'Actual followup upload/download');
 await renderedElements(page,'.photo-preview',1);
 await call(page,'onSymptoms',{detail:{value:'虚构反馈：没有不适'}});
 await call(page,'submit');
 data=await waitUntil(async()=>{const d=await pageData(page.pageId);if(d.error)throw new Error(d.error);return !d.saving && d.active.status==='submitted'?d:null;},'Native symptoms submitted');
 assert.equal(data.active.photos.length,1);await renderedText(page,'.symptoms-result','虚构反馈：没有不适');
 await api(`/admin/followups/${data.active.id}/review`,{token:adminToken,method:'POST',body:{version:data.active.version,action:'review',response:'虚构医护普通回评',disposition:''}});
 await call(page,'load');data=await readyData(page.pageId);assert.equal(data.active.status,'reviewed');await renderedText(page,'.review-response','虚构医护普通回评');
 const snapshot=data.active.photos[0].id;
 await call(page,'selectTask',{currentTarget:{dataset:{id:recorded.tasks[1].id}}});
 await waitUntil(async()=> (await pageData(page.pageId)).active.id===recorded.tasks[1].id,'Select second task');
 await call(page,'onSymptoms',{detail:{value:'虚构反馈：明显不适'}});await call(page,'onSeverity',{detail:{value:'2'}});
 await call(page,'submit');
 data=await waitUntil(async()=>{const d=await pageData(page.pageId);if(d.error)throw new Error(d.error);return !d.saving && d.active.status==='alert'?d:null;},'Native alert submission');
 await renderedElements(page,'.attention',1);
 await api(`/admin/followups/${data.active.id}/review`,{token:adminToken,method:'POST',body:{version:data.active.version,action:'handle',response:'虚构异常回评',disposition:'已核实并安排演示复诊'}});
 await call(page,'load');data=await readyData(page.pageId);assert.equal(data.active.status,'handled');assert.equal(data.active.disposition,'已核实并安排演示复诊');await renderedText(page,'.review-response','虚构异常回评');
 await call(page,'revoke');await waitUntil(async()=> !(await pageData(page.pageId)).active.photosGranted,'Revoke after treatment completion');
 assert.equal((await fetch(`${apiBase}/admin/followup-photos/${snapshot}/content`,{headers:{Authorization:`Bearer ${adminToken}`}})).status,403);
 assert.equal((await fetch(`${apiBase}/me/followup-photos/${snapshot}/content`,{headers:{Authorization:`Bearer ${session.token}`}})).status,200);
 await call(page,'selectTask',{currentTarget:{dataset:{id:recorded.tasks[0].id}}});
 await waitUntil(async()=>{const d=await pageData(page.pageId);return d.active.id===recorded.tasks[0].id && d.photos[0].id===snapshot && d.photos[0].path;},'Frozen earlier task photo retained');
 await renderedElements(page,'.photo-preview',1);
 assert.equal(exceptions.length,0,JSON.stringify(exceptions));
 console.log('PASS: native task list → consent → actual photo upload → symptoms → staff review → patient rendered response → alert → explicit handling → revocation and retained prior snapshot.');
} catch(error){console.error('Followup scenario:',error);throw error;}
finally{
 const failures=[];
 for(const [method,active] of [['chooseImage',chooseMocked],['showModal',modalMocked]])if(active)try{await send('App.mockWxMethod',{method});}catch{failures.push('restore '+method);}
 if(appointment)try{const latest=(await api(`/me/appointments/${appointment.id}`,{token:session.token})).data;if(latest.canCancel)await api(`/me/appointments/${appointment.id}/cancel`,{token:session.token,method:'POST',body:{version:latest.version}});}catch{failures.push('release test reservation');}
 if(storageIsolated && connected)try{
  await evaluate(function route(){wx.reLaunch({url:'/pages/doctors/index'});return true;});
  try{await currentPage('pages/doctors/index');}catch{/* Restore owned storage even when simulator navigation is unavailable. */}
  await evaluate(function restore(marker){
   const app=getApp(),state=app.__blkM3Smoke;if(!state || state.marker!==marker)throw new Error('Cleanup marker mismatch');
   // Invalidate private page callbacks before restoring the original identity.
   for(const page of getCurrentPages()){if(typeof page.generation==='number')page.generation++;page.disposed=true;}
   for(const backup of state.backups){if(backup.present)wx.setStorageSync(backup.key,backup.value);else wx.removeStorageSync(backup.key);}
   try{wx.getFileSystemManager().unlinkSync(wx.env.USER_DATA_PATH+'/m3-synthetic.png');}catch{}
   delete app.__blkM3Smoke;return true;
  },isolationMarker);
  console.log('Restored original session/storage. Completed test treatment released its slot; demo audit and tasks remain.');
 }catch(error){console.error(error.message);failures.push('restore original storage');}
 socket.close();for(const callback of pending.values())clearTimeout(callback.timer);
 if(failures.length)throw new Error('Cleanup requires attention: '+failures.join(', '));
}
