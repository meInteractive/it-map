import {readClassificationSettings} from '../repositories/classification-prompts.js';
import {readVacancyRoleCatalog} from '../repositories/vacancy-roles.js';

// A deliberately broad source-side prefilter. JEV still makes the semantic
// decision; no sector, salary or personal-profile restrictions are added here.
export const HH_TITLE_TERMS=Object.freeze({
 'root-it-manager':['руководител*','директор*','лид*','менеджер*','начальник*','управля*','владел*','продукт*','проект*','продакт*','проджект*','тимлид*','teamlead*','head','lead*','manager','director','owner','product','project','program','delivery','chief','CTO','CPO','CIO'],
 'root-journalist':['журналист*','редактор*','корреспондент*','обозревател*','спецкор*','автор*','editor','journalist','reporter','writer','columnist','correspondent']
});
export function hhTitleQuery(rootKeys){
 if(!rootKeys.length||rootKeys.some(k=>!Object.hasOwn(HH_TITLE_TERMS,k)))return null;
 const terms=[...new Set([...new Set(rootKeys)].sort().flatMap(k=>HH_TITLE_TERMS[k]))];
 return 'NAME:('+terms.join(' OR ')+')';
}
export async function readHHTitleFilter(db){
 const settings=await readClassificationSettings(db),catalog=await readVacancyRoleCatalog(db);
 const active=settings.roles.filter(r=>r.enabled&&r.markdown.trim()),rootKeys=active.map(r=>catalog.byId.get(r.roleId)?.stableKey),query=hhTitleQuery(rootKeys);
 return {version:1,query,rootRoleIds:active.map(r=>r.roleId),status:query?'applied':active.length?'unmapped_role':'no_active_roles'};
}
// Only fixed preset identity is disclosed; never expose arbitrary saved text.
export function hhTitleFilterEvidence(text){
 if(typeof text!=='string'||!text.trim())return 'none';
 for(const keys of [['root-it-manager'],['root-journalist'],['root-it-manager','root-journalist']])if(text===hhTitleQuery(keys))return 'role_titles_v1';
 return 'custom';
}
