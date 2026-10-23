import { ApiError, errorMessage } from '../../services/api';
import { cachedSession } from '../../services/patient';
import { assessmentApi, assessmentLabels, assessmentFailureLabels, assessmentKey, assessmentTime, type AssessmentView, type AssessmentTask } from '../../services/assessment-api';
type TaskDisplay = AssessmentTask & {label:string;time:string};
type Pending = { action:'retry'|'cancel'|'consent'; version:number; key:string };
Page({
 data:{tasks:[] as TaskDisplay[],selected:null as AssessmentView|null,label:'',failureMessage:'',error:'',needsLogin:false,busy:false,loading:false,pending:false,statuses:['','draft','queued','analyzing','retry_wait','awaiting_review','completed','rejected','failed','cancelled'],filters:['全部','准备照片','等待分析','正在分析','等待重试','待医生复核','复核完成','需补拍或面诊','分析失败','已取消'],filter:0,page:0,total:0},
 id:'',patientId:'',generation:0,visible:false,timer:undefined as ReturnType<typeof setTimeout>|undefined,operation:null as Pending|null,
 onLoad(options:Record<string,string|undefined>){this.id=options.id||'';},
 onShow(){this.visible=true;void this.load();},
 onHide(){this.visible=false;this.generation++;if(this.timer)clearTimeout(this.timer);this.operation=null;this.setData({selected:null,tasks:[],busy:false,loading:false,pending:false});},
 onUnload(){this.onHide();},
 onPullDownRefresh(){void this.load().finally(()=>wx.stopPullDownRefresh());},
 onReachBottom(){if(!this.id&&!this.data.loading&&this.data.tasks.length<this.data.total)void this.load(false,true);},
 storage(){return `assessment-operation-v1:${this.patientId}:${this.id}`;},
 schedule(){if(this.timer)clearTimeout(this.timer);if(this.visible)this.timer=setTimeout(()=>void this.load(true),5000);},
 async load(poll=false,more=false){
  if(!this.visible||this.data.busy||this.data.loading){this.schedule();return;}
  const session=cachedSession();if(!session){this.setData({needsLogin:true,selected:null,tasks:[]});return;}
  if(this.patientId&&session.patient.id!==this.patientId){this.id='';this.operation=null;this.setData({tasks:[],selected:null,pending:false});}
  this.patientId=session.patient.id;const generation=++this.generation;this.setData({loading:true,needsLogin:false,...(!poll?{error:''}:{})});
  try{
   if(this.id){const view=await assessmentApi.get(this.id,this.patientId);if(generation!==this.generation)return;this.setData({selected:view,label:assessmentLabels[view.task.status],failureMessage:assessmentFailureLabels[view.task.failureCode]||'暂未完成，请稍后刷新'});const pending=wx.getStorageSync(this.storage()) as Pending|undefined;if(pending){this.operation=pending;this.setData({pending:true});}}
   else{const page=more?this.data.page+1:1;const result=await assessmentApi.list(this.patientId,page,this.data.statuses[this.data.filter]||'');if(generation!==this.generation)return;const rows=(result.data||[]).map(t=>({...t,label:assessmentLabels[t.status],time:assessmentTime(t.updatedAt)}));this.setData({tasks:more?[...this.data.tasks,...rows]:rows,page,total:result.meta?.total||0});}
  }catch(e){if(generation===this.generation){this.setData({error:errorMessage(e)});if(e instanceof ApiError&&[401,403,404].includes(e.status))this.setData({tasks:[],selected:null});}}
  finally{if(generation===this.generation){this.setData({loading:false});this.schedule();}}
 },
 login(){wx.navigateTo({url:'/pages/profile/index'});},
 filterChanged(e:WechatMiniprogram.CustomEvent<{value:string}>){this.generation++;this.setData({loading:false});this.setData({filter:Number(e.detail.value),tasks:[],page:0});void this.load();},
 open(e:WechatMiniprogram.TouchEvent){wx.navigateTo({url:`/pages/assessment-tasks/index?id=${encodeURIComponent(String(e.currentTarget.dataset.id))}`});},
 back(){this.generation++;this.setData({loading:false});this.id='';this.operation=null;this.setData({selected:null,pending:false});void this.load();},
 result(){wx.navigateTo({url:`/pages/assessment-result/index?id=${encodeURIComponent(this.id)}`});},
 prepare(){wx.navigateTo({url:`/pages/assessment/index?id=${encodeURIComponent(this.id)}`});},
 retake(){const task=this.data.selected?.task;if(task)wx.navigateTo({url:`/pages/assessment/index?supersedes=${encodeURIComponent(task.id)}&clinicId=${encodeURIComponent(task.clinicId)}`});},
 action(e:WechatMiniprogram.TouchEvent){
  const action=String(e.currentTarget.dataset.action) as Pending['action'];if(this.data.busy||this.operation||!this.data.selected)return;
  if(action==='cancel'||action==='consent'){const generation=this.generation;wx.showModal({title:action==='cancel'?'取消评估任务':'撤回AI授权',content:'该任务将停止后续分析；重新评估需要创建新任务。',success:r=>{if(r.confirm&&generation===this.generation)void this.perform(action);}});}else void this.perform(action);
 },
 async perform(action?:Pending['action']){
  const view=this.data.selected;if(!view||this.data.busy)return;if(!this.operation){if(!action)return;this.operation={action,version:view.task.version,key:assessmentKey()};wx.setStorageSync(this.storage(),this.operation);}
  const pending=this.operation,generation=++this.generation;this.setData({busy:true,error:'',pending:true,loading:false});
  try{const next=await assessmentApi.change(this.id,pending.action,pending.action==='consent'?{version:pending.version,policyVersion:view.consent.version,granted:false}:{version:pending.version},pending.key,this.patientId);if(generation!==this.generation)return;wx.removeStorageSync(this.storage());this.operation=null;this.setData({selected:next,label:assessmentLabels[next.task.status],failureMessage:assessmentFailureLabels[next.task.failureCode]||'',pending:false});}
  catch(e){if(generation===this.generation){this.setData({error:errorMessage(e)});if(e instanceof ApiError&&e.status>=400&&e.status<500){wx.removeStorageSync(this.storage());this.operation=null;this.setData({pending:false});}}}
  finally{if(generation===this.generation){this.setData({busy:false});void this.load(true);}}
 },
 retryOperation(){void this.perform();},
});
