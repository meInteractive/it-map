import {validateLeafRoleIds} from '../server/services/vacancy-leaf-roles.js';
const allowed=[{id:2,path:'CI / Two'},{id:1,path:'CI / One'},{id:3,path:'CI / Three'}];
const cases=[
 ['valid',JSON.stringify({role_ids:[3,1]})],['empty',JSON.stringify({role_ids:[]})],['reverse',JSON.stringify({role_ids:[2,1]})],
 ['duplicate',JSON.stringify({role_ids:[1,1]})],['unknown',JSON.stringify({role_ids:[4]})],['decimal',JSON.stringify({role_ids:[1.5]})],
 ['string',JSON.stringify({role_ids:['1']})],['null',JSON.stringify({role_ids:null})],['extra',JSON.stringify({role_ids:[1],comment:'CI'})],
 ['object','{}'],['malformed','{'],['array','[]'],['negative',JSON.stringify({role_ids:[-1]})],
 ['too_many',JSON.stringify({role_ids:Array.from({length:13},()=>1)})],['unsafe',JSON.stringify({role_ids:[9007199254740992]})],
];
console.log(JSON.stringify(cases.map(([name,content])=>{try{return{name,content,ok:true,ids:validateLeafRoleIds(content,allowed)}}catch(e){return{name,content,ok:false,code:e.code,status:e.status,message:e.message}}}))); 
