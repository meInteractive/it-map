// A bounded projection for owner diagnostics. Never expose result bodies,
// prompts or arbitrary provider fields from the durable attempt receipt.
export function schedulerAttemptDetails(row){
 const {result_json,...attempt}=row;let result;
 try{result=JSON.parse(result_json);}catch{return attempt;}
 if(attempt.type==='classify_title'&&typeof result?.accepted==='boolean')attempt.outcome={accepted:result.accepted};
 if(attempt.type==='assign_roles'&&Array.isArray(result?.roleIds)&&result.roleIds.length<=12&&result.roleIds.every(id=>Number.isSafeInteger(id)&&id>0)&&new Set(result.roleIds).size===result.roleIds.length)attempt.outcome={roleIds:result.roleIds,filtered:result.roleIds.length===0};
 if(attempt.type==='import_vacancy'&&result?.superseded===true&&(result.nextStage===null||['classify_title','fetch_full_text','extract_vacancy','assign_roles','import_vacancy','score_relevance'].includes(result.nextStage)))attempt.outcome={superseded:true,nextStage:result.nextStage};
 return attempt;
}
