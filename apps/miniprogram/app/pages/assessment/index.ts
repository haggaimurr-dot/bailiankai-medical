import { api, errorMessage, ApiError, type Clinic } from '../../services/api';
import { cachedSession } from '../../services/patient';
import { assessmentApi, assessmentAngles, assessmentQualityLabels, assessmentKey, type AssessmentView } from '../../services/assessment-api';
import type { components } from '../../../../../packages/contracts/src/schema';
type PendingAction = { action: 'consent'|'submit'|'quality-check'|'cancel'; body: components['schemas']['AssessmentVersion'] | components['schemas']['AssessmentConsentChange']; key: string };
type PendingUpload = { angle: string; filePath: string; version: number; key: string };
Page({
 data: { clinics: [] as Clinic[], clinicIndex:0, view:null as AssessmentView|null, angles:assessmentAngles.map(a=>({...a,photoId:'',quality:'未上传',advice:[] as string[]})), error:'', busy:false, needsLogin:false, qualityAvailable:false, modelAvailable:false, actionPending:false, uploadPending:false, canSubmit:false, recovering:false },
 id:'', patientId:'', generation:0, visible:false, supersedes:'', requestedClinicId:'', pendingAction:null as PendingAction|null, pendingUpload:null as PendingUpload|null, previews:[] as string[], timer:undefined as ReturnType<typeof setTimeout>|undefined,
 onLoad(options:Record<string,string|undefined>){this.id=options.id||'';this.supersedes=options.supersedes||'';this.requestedClinicId=options.clinicId||'';},
 onShow(){this.visible=true;void this.load();},
 onHide(){this.visible=false;this.generation++;if(this.timer)clearTimeout(this.timer);this.pendingAction=null;this.pendingUpload=null;this.clearPreviews();this.setData({view:null,busy:false,actionPending:false,uploadPending:false,angles:assessmentAngles.map(a=>({...a,photoId:'',quality:'未上传',advice:[]}))});},
 onUnload(){this.onHide();},
 clearPreviews(){for(const path of this.previews)try{wx.getFileSystemManager().unlink({filePath:path});}catch{}this.previews=[];},
 apply(view:AssessmentView){this.setData({view,angles:assessmentAngles.map(a=>{const p=view.photos.find(p=>p.angle===a.value);return{...a,photoId:p?.id||'',quality:p?assessmentQualityLabels[p.qualityCheck.status]||'检查暂不可用':'未上传',advice:p?.qualityCheck.retakeAdvice||[]};}),canSubmit:view.task.status==='draft'&&view.consent.granted&&view.photos.length===3&&view.photos.every(p=>p.qualityCheck.status==='passed')&&this.data.modelAvailable});},
 schedule(){if(this.timer)clearTimeout(this.timer);if(this.visible&&this.id)this.timer=setTimeout(()=>{void this.load(true);},5000);},
 async load(poll=false){
  if(!this.visible||this.data.busy){this.schedule();return;}
  const session=cachedSession();if(!session){this.patientId='';this.setData({needsLogin:true,view:null});return;}
  if(this.patientId&&this.patientId!==session.patient.id){this.id='';this.pendingAction=null;this.pendingUpload=null;this.clearPreviews();}
  this.patientId=session.patient.id;const generation=++this.generation;this.setData({needsLogin:false,...(!poll?{error:''}:{})});
  try{
   if(!poll){const [clinics,capabilities]=await Promise.all([api.clinics(),assessmentApi.capabilities()]);if(generation!==this.generation)return;const wanted=this.requestedClinicId||this.data.view?.task.clinicId;this.setData({clinics,clinicIndex:Math.max(0,clinics.findIndex(c=>c.id===wanted)),qualityAvailable:capabilities.qualityAvailable,modelAvailable:capabilities.modelAvailable,recovering:!!wx.getStorageSync(`assessment-create-v1:${this.patientId}`)});}
   if(this.id){const view=await assessmentApi.get(this.id,this.patientId);if(generation!==this.generation)return;this.apply(view);}
  }catch(e){if(generation===this.generation){this.setData({error:errorMessage(e)});if(e instanceof ApiError&&[401,403,404].includes(e.status))this.setData({view:null});}}
  finally{if(generation===this.generation)this.schedule();}
 },
 login(){wx.navigateTo({url:'/pages/profile/index'});},
 clinicChanged(e:WechatMiniprogram.CustomEvent<{value:string}>){if(!this.id&&!this.data.recovering)this.setData({clinicIndex:Number(e.detail.value)});},
 async create(){
  if(this.data.busy||this.id||!this.patientId)return;
  const key=`assessment-create-v1:${this.patientId}`;let body=wx.getStorageSync(key) as components['schemas']['AssessmentCreate']|undefined;
  if(!body){const clinic=this.data.clinics[this.data.clinicIndex];if(!clinic)return;body={clinicId:clinic.id,requestKey:assessmentKey(),...(this.supersedes?{supersedesAssessmentId:this.supersedes}:{})};wx.setStorageSync(key,body);}
  const generation=++this.generation;this.setData({busy:true,error:'',recovering:true});
  try{const view=await assessmentApi.create(body,this.patientId);if(generation!==this.generation)return;this.id=view.task.id;wx.removeStorageSync(key);this.setData({recovering:false});this.apply(view);}
  catch(e){if(generation===this.generation){this.setData({error:errorMessage(e)});if(e instanceof ApiError&&e.status>=400&&e.status<500){wx.removeStorageSync(key);this.setData({recovering:false});}}}
  finally{if(generation===this.generation){this.setData({busy:false});this.schedule();}}
 },
 async perform(action?:PendingAction['action'],granted?:boolean){
  if(!this.data.view||this.data.busy||!this.patientId)return;
  if(!this.pendingAction){if(!action)return;this.pendingAction={action,key:assessmentKey(),body:action==='consent'?{version:this.data.view.task.version,policyVersion:this.data.view.consent.version,granted:!!granted}:{version:this.data.view.task.version}};}
  const operation=this.pendingAction,generation=++this.generation;this.setData({busy:true,error:'',actionPending:true});
  try{const view=await assessmentApi.change(this.id,operation.action,operation.body,operation.key,this.patientId);if(generation!==this.generation)return;this.pendingAction=null;this.setData({actionPending:false});this.apply(view);}
  catch(e){if(generation===this.generation){this.setData({error:errorMessage(e)});if(e instanceof ApiError&&e.status>=400&&e.status<500){this.pendingAction=null;this.setData({actionPending:false});}}}
  finally{if(generation===this.generation){this.setData({busy:false});void this.load(true);}}
 },
 consent(){const view=this.data.view;if(!view||this.data.busy)return;const generation=this.generation;wx.showModal({title:view.consent.granted?'撤回AI用途授权':'AI用途授权',content:view.consent.granted?'撤回后停止本任务分析和医护访问，重新评估需创建新任务。':`${view.consent.content}\n版本：${view.consent.version}`,confirmText:view.consent.granted?'撤回':'同意',success:r=>{if(r.confirm&&generation===this.generation)void this.perform('consent',!view.consent.granted);}});},
 submit(){void this.perform('submit');},check(){void this.perform('quality-check');},retryAction(){void this.perform();},
 async choose(e:WechatMiniprogram.TouchEvent){
  if(!this.data.view?.consent.granted||this.data.view.task.status!=='draft'||this.data.busy||this.pendingAction||this.pendingUpload)return;
  const angle=String(e.currentTarget.dataset.angle),generation=this.generation;
  wx.chooseMedia({count:1,mediaType:['image'],sourceType:['camera','album'],success:r=>{if(generation!==this.generation||!this.visible)return;const file=r.tempFiles[0];if(!file)return;this.pendingUpload={angle,filePath:file.tempFilePath,version:this.data.view!.task.version,key:assessmentKey()};void this.upload();}});
 },
 async upload(){
  const operation=this.pendingUpload;if(!operation||this.data.busy)return;const generation=++this.generation;this.setData({busy:true,error:'',uploadPending:true});
  try{const view=await assessmentApi.upload(this.id,operation.angle,operation.filePath,operation.version,operation.key,this.patientId);if(generation!==this.generation)return;this.pendingUpload=null;this.setData({uploadPending:false});this.apply(view);}
  catch(e){if(generation===this.generation){this.setData({error:errorMessage(e)});if(e instanceof ApiError&&e.status===409&&e.code!=='PATIENT_IDENTITY_CHANGED'){try{const current=await assessmentApi.get(this.id,this.patientId);if(generation!==this.generation)return;this.apply(current);this.pendingUpload={...operation,version:current.task.version,key:assessmentKey()};}catch{this.pendingUpload=null;this.setData({uploadPending:false});}}else if(e instanceof ApiError&&e.status>=400&&e.status<500){this.pendingUpload=null;this.setData({uploadPending:false});}}}
  finally{if(generation===this.generation){this.setData({busy:false});this.schedule();}}
 },
 async preview(e:WechatMiniprogram.TouchEvent){const photoId=String(e.currentTarget.dataset.id);if(!photoId)return;const generation=this.generation;try{const path=await assessmentApi.photo(this.id,photoId,this.patientId);if(generation!==this.generation||!this.visible){try{wx.getFileSystemManager().unlink({filePath:path});}catch{}return;}this.previews.push(path);wx.previewImage({urls:[path],current:path});}catch(e){if(generation===this.generation)this.setData({error:errorMessage(e)});}},
 task(){if(this.id)wx.navigateTo({url:`/pages/assessment-tasks/index?id=${encodeURIComponent(this.id)}`});},
});
