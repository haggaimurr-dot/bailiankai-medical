import { errorMessage } from '../../services/api';
import { cachedSession } from '../../services/patient';
import { assessmentApi, assessmentLabels, assessmentTime, type AssessmentView } from '../../services/assessment-api';
Page({
 data:{review:null as AssessmentView['publishedReview']|null,label:'',error:'',time:'',needsLogin:false,clinicId:''},id:'',generation:0,visible:false,timer:undefined as ReturnType<typeof setTimeout>|undefined,
 onLoad(options:Record<string,string|undefined>){this.id=options.id||'';},
 onShow(){this.visible=true;void this.load();},
 onHide(){this.visible=false;this.generation++;if(this.timer)clearTimeout(this.timer);this.setData({review:null,label:'',time:'',clinicId:''});},onUnload(){this.onHide();},
 onPullDownRefresh(){void this.load().finally(()=>wx.stopPullDownRefresh());},
 async load(){if(!this.visible)return;if(this.timer)clearTimeout(this.timer);this.timer=undefined;const generation=++this.generation;this.setData({review:null,label:'',error:'',time:''});const session=cachedSession();if(!session){this.setData({needsLogin:true});return;}try{const view=await assessmentApi.get(this.id,session.patient.id);if(generation!==this.generation)return;const review=view.publishedReview;this.setData({review:review||null,label:assessmentLabels[view.task.status],time:review?assessmentTime(review.reviewedAt):'',needsLogin:false,clinicId:view.task.clinicId});}catch(e){if(generation===this.generation)this.setData({error:errorMessage(e),review:null});}finally{if(generation===this.generation&&this.visible)this.timer=setTimeout(()=>void this.load(),5000);}},
 login(){wx.navigateTo({url:'/pages/profile/index'});},task(){wx.navigateTo({url:`/pages/assessment-tasks/index?id=${encodeURIComponent(this.id)}`});},retake(){wx.navigateTo({url:`/pages/assessment/index?supersedes=${encodeURIComponent(this.id)}&clinicId=${encodeURIComponent(this.data.clinicId)}`});},
});
