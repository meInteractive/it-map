import {lazy,Suspense} from 'react';
import {useSectionState} from '../../hooks/useSectionState';
import {confirmNavigation} from '../../hooks/useDirty';
const OpenRouter=lazy(()=>import('./OpenRouter')),OpenAI=lazy(()=>import('./OpenAI'));
export default function AIProviders(){
 const [provider,setProvider]=useSectionState('settings:ai-provider','openrouter');
 return <><div className="settings-tabs" role="tablist" aria-label="Провайдер нейросети">{[['openrouter','OpenRouter'],['openai','ChatGPT']].map(([key,label])=><button key={key} role="tab" aria-selected={provider===key} onClick={()=>{if(provider===key||confirmNavigation())setProvider(key);}}>{label}</button>)}</div><Suspense fallback={<div className="empty">Загрузка…</div>}>{provider==='openai'?<OpenAI/>:<OpenRouter/>}</Suspense></>;
}
