import { errorMessage } from '../../services/api';
import { confirmDialog, confirmNewSession, isSessionExpired, patientApi, type CarePhoto, type FollowupTask, type FollowupWorkspace, type TreatmentRevision } from '../../services/patient';
type Angle = CarePhoto['angle'];
interface TaskView extends FollowupTask { dueLabel: string; statusLabel: string; }
interface RevisionView extends TreatmentRevision { changedLabel: string; treatedLabel: string; }
interface PhotoView { angle: Angle; label: string; id: string; path: string; }
interface FollowupData {
 loading: boolean; saving: boolean; uploading: boolean; consentLoading: boolean; expired: boolean; error: string; notice: string;
 patientId: string; treatment: FollowupWorkspace['treatment']; revisions: RevisionView[]; tasks: TaskView[]; active: TaskView | null;
 symptoms: string; severity: number; concern: boolean; photos: PhotoView[]; page: number; total: number; appointmentMode: boolean;
 severityLabels: string[];
}
interface FollowupMethods {
 appointmentId: string; requestedTaskId: string; generation: number; disposed: boolean;
 load(): Promise<void>; selectTask(event: WechatMiniprogram.TouchEvent): void; useTask(task: TaskView, preserve?: boolean): Promise<void>;
 onSymptoms(event: WechatMiniprogram.Input): void; onSeverity(event: WechatMiniprogram.PickerChange): void; onConcern(event: WechatMiniprogram.SwitchChange): void;
 choose(event: WechatMiniprogram.TouchEvent): void; upload(taskId: string, angle: Angle, path: string): Promise<void>;
 preview(event: WechatMiniprogram.TouchEvent): void; submit(): Promise<void>; grant(): Promise<void>; revoke(): Promise<void>; consent(granted: boolean): Promise<void>;
 retry(): void; next(): void; previous(): void; openPlan(): void; newIdentity(): Promise<void>;
}
const labels: Record<string,string> = { pending: '待填写', submitted: '等待医护回评', alert: '等待医护处理', reviewed: '医护已回评', handled: '异常已处理' };
function view(task: FollowupTask): TaskView { return { ...task, statusLabel: labels[task.status], dueLabel: new Intl.DateTimeFormat('zh-CN',{timeZone:'Asia/Shanghai',year:'numeric',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',hour12:false}).format(new Date(task.dueAt)) }; }
function dateLabel(value: string): string { return new Intl.DateTimeFormat('zh-CN',{timeZone:'Asia/Shanghai',year:'numeric',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',hour12:false}).format(new Date(value)); }
function revisionView(item: TreatmentRevision): RevisionView { return {...item,changedLabel:dateLabel(item.changedAt),treatedLabel:dateLabel(item.treatedAt)}; }
function blanks(): PhotoView[] { return [{ angle:'front',label:'正面',id:'',path:'' },{ angle:'left',label:'左侧',id:'',path:'' },{ angle:'right',label:'右侧',id:'',path:'' }]; }
Page<FollowupData,FollowupMethods>({
 data: { loading:true,saving:false,uploading:false,consentLoading:false,expired:false,error:'',notice:'',patientId:'',treatment:null,revisions:[],tasks:[],active:null,symptoms:'',severity:0,concern:false,photos:blanks(),page:1,total:0,appointmentMode:false,severityLabels:['没有不适','轻微不适','明显不适','严重不适'] },
 appointmentId:'',requestedTaskId:'',generation:0,disposed:false,
 onLoad(options) { this.appointmentId=options.id || ''; this.requestedTaskId=options.taskId || ''; this.setData({appointmentMode:Boolean(this.appointmentId)}); void this.load(); },
 onShow() { if (!this.data.loading && !this.data.saving && !this.data.uploading) void this.load(); },
 onUnload() { this.generation++;this.disposed=true; },
 onPullDownRefresh() { if (this.data.saving || this.data.uploading || this.data.consentLoading) wx.stopPullDownRefresh(); else void this.load(); },
 async load() {
  if (this.disposed || this.data.saving || this.data.uploading || this.data.consentLoading) return;
  const generation=++this.generation;this.setData({loading:true,error:'',expired:false});
  try {
   const identity=await patientApi.me();if (generation!==this.generation)return;
   const preserve=this.data.patientId===identity.id;
   let tasks: FollowupTask[],treatment: FollowupWorkspace['treatment']=null,revisions: RevisionView[]=[],total=0;
   if (this.appointmentId) {const workspace=await patientApi.followupWorkspace(this.appointmentId,identity.id);tasks=workspace.tasks;treatment=workspace.treatment;revisions=workspace.treatmentRevisions.map(revisionView);total=tasks.length;}
   else {const result=await patientApi.followups('',this.data.page,identity.id);tasks=result.data || [];total=result.meta?.total || 0;}
   if (generation!==this.generation)return;
   const rendered=tasks.map(view),selected=rendered.find(item=>preserve && item.id===this.data.active?.id) || rendered.find(item=>item.id===this.requestedTaskId) || rendered[0] || null;
   this.setData({patientId:identity.id,tasks:rendered,treatment,revisions,total});
   if(selected)await this.useTask(selected,preserve && selected.id===this.data.active?.id);else this.setData({active:null,photos:blanks()});
  } catch(error) {if(generation===this.generation)this.setData({error:errorMessage(error),expired:isSessionExpired(error)});}
  finally {if(generation===this.generation){this.setData({loading:false});wx.stopPullDownRefresh();}}
 },
 selectTask(event) {
  if(this.data.saving || this.data.uploading || this.data.consentLoading)return;
  const task=this.data.tasks.find(item=>item.id===String(event.currentTarget.dataset.id));
  if(task)void this.useTask(task);
 },
 async useTask(task,preserve=false) {
  const generation=this.generation;
  this.setData({active:task,photos:blanks(),notice:'',...(preserve && task.status==='pending'?{}:{symptoms:task.symptoms,severity:task.severity,concern:task.concern})});
  const photos=blanks();
  try {
   if(!this.appointmentId) {
    const workspace=await patientApi.followupWorkspace(task.appointmentId,this.data.patientId);
    if(generation!==this.generation || this.data.active?.id!==task.id)return;
    this.setData({treatment:workspace.treatment,revisions:workspace.treatmentRevisions.map(revisionView)});
   }
   await Promise.all(task.photos.map(async photo=>{const item=photos.find(entry=>entry.angle===photo.angle);if(item){item.id=photo.id;item.path=await patientApi.followupPhotoFile(photo.id,this.data.patientId);}}));
   if(generation===this.generation && this.data.active?.id===task.id)this.setData({photos});
  }catch(error){if(generation===this.generation && this.data.active?.id===task.id)this.setData({error:errorMessage(error),expired:isSessionExpired(error)});}
 },
 onSymptoms(event){this.setData({symptoms:event.detail.value});},
 onSeverity(event){this.setData({severity:Number(event.detail.value)});},
 onConcern(event){this.setData({concern:event.detail.value});},
 choose(event){
  const task=this.data.active;
  if(!task || task.status!=='pending' || !task.photosGranted || this.data.uploading || this.data.saving || this.data.consentLoading)return;
  const angle=String(event.currentTarget.dataset.angle) as Angle;if(!this.data.photos.some(item=>item.angle===angle))return;
  wx.chooseImage({count:1,sizeType:['original'],sourceType:['album','camera'],success:result=>{const path=result.tempFilePaths[0];if(path && !this.disposed && this.data.active?.id===task.id)void this.upload(task.id,angle,path);}});
 },
 async upload(taskId,angle,path){
  if(this.data.uploading || this.data.saving || this.disposed || this.data.active?.id!==taskId)return;
  this.setData({uploading:true,error:'',notice:''});const generation=this.generation;
  try{
   const photo=await patientApi.uploadFollowupPhoto(taskId,angle,path,this.data.patientId);
   const localPath=await patientApi.followupPhotoFile(photo.id,this.data.patientId);
   if(generation!==this.generation || this.data.active?.id!==taskId)return;
   const task=this.data.active!;task.photos=task.photos.filter(item=>item.angle!==angle).concat(photo);
   this.setData({active:task,photos:this.data.photos.map(item=>item.angle===angle?{...item,id:photo.id,path:localPath}:item),notice:'照片已保存，提交反馈后本次照片将保留。'});
  }catch(error){if(generation===this.generation)this.setData({error:errorMessage(error),expired:isSessionExpired(error)});}
  finally{if(generation===this.generation)this.setData({uploading:false});}
 },
 preview(event){const path=this.data.photos.find(item=>item.angle===String(event.currentTarget.dataset.angle))?.path;if(path)wx.previewImage({current:path,urls:[path]});},
 async submit(){
  const task=this.data.active;
  if(!task || task.status!=='pending' || this.data.saving || this.data.uploading || this.data.consentLoading)return;
  if(!this.data.symptoms.trim()){this.setData({error:'请描述当前情况；没有不适也可以填写。'});return;}
  this.setData({saving:true,error:'',notice:''});const generation=this.generation;
  try{
   const result=await patientApi.submitFollowup(task.id,{version:task.version,symptoms:this.data.symptoms.trim(),severity:this.data.severity,concern:this.data.concern},this.data.patientId);
   if(generation!==this.generation)return;
   const rendered=view(result);this.setData({active:rendered,tasks:this.data.tasks.map(item=>item.id===task.id?rendered:item),notice:result.status==='alert'?'已提交并进入医护关注队列。':'反馈已提交，请等待医护回评。'});
  }catch(error){if(generation===this.generation)this.setData({error:errorMessage(error)+' 可重试相同反馈或刷新查看提交结果。',expired:isSessionExpired(error)});}
  finally{if(generation===this.generation)this.setData({saving:false});}
 },
 grant(){return this.consent(true);},revoke(){return this.consent(false);},
 async consent(granted){
  const task=this.data.active;if(!task || this.data.consentLoading || this.data.uploading || this.data.saving)return;
  this.setData({consentLoading:true,error:''});const generation=this.generation;
  try{
   const accepted=await confirmDialog(granted?'照片诊疗用途授权':'撤回照片授权',granted?'照片用于本次诊疗及治疗后随访，授权记录保留，可随时撤回。':'撤回后医护不能查看本次预约的面诊及随访照片，你仍可查看本人照片。',granted?'同意并继续':'撤回授权');if(!accepted)return;
   await patientApi.setPhotoConsent(task.appointmentId,granted,this.data.patientId);
   if(generation===this.generation)this.setData({active:{...task,photosGranted:granted},tasks:this.data.tasks.map(item=>item.appointmentId===task.appointmentId?{...item,photosGranted:granted}:item)});
  }catch(error){if(generation===this.generation)this.setData({error:errorMessage(error),expired:isSessionExpired(error)});}
  finally{if(generation===this.generation)this.setData({consentLoading:false});}
 },
 retry(){void this.load();},next(){if(!this.data.loading && !this.data.saving && !this.data.uploading && this.data.page*20<this.data.total){this.setData({page:this.data.page+1,active:null});void this.load();}},previous(){if(!this.data.loading && !this.data.saving && !this.data.uploading && this.data.page>1){this.setData({page:this.data.page-1,active:null});void this.load();}},
 openPlan(){const id=this.appointmentId || this.data.active?.appointmentId;if(id)wx.navigateTo({url:`/pages/care-plan/index?id=${encodeURIComponent(id)}&versionId=${encodeURIComponent(this.data.treatment?.planVersionId || '')}`});},
 async newIdentity(){if(await confirmNewSession())void this.load();},
});
