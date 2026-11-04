import {test,expect} from '@playwright/test';

const jobID='12345678-1234-4234-8234-123456789abd';
test('ручное продолжение отправляет только выбранное HH-задание',async({page,isMobile})=>{
 const calls:{path:string;body:Record<string,unknown>}[]=[];
 await page.route(url=>url.pathname.startsWith('/api/'),async route=>{
  const req=route.request(),url=new URL(req.url()),path=url.pathname;
  if(path==='/api/scheduler/resume'&&req.method()==='POST'){
   calls.push({path,body:req.postDataJSON()});
   return route.fulfill({json:{status:'completed',processed:1}});
  }
  if(path==='/api/scheduler/queue')return route.fulfill({json:{jobs:[{id:jobID,type:'scan_source',company_id:1,candidate_id:null,source_id:'hh',priority:20,status:'pending',run_after:0,attempts:0,max_attempts:4,last_error:'',companyName:'CI test'}],total:1,page:1,pageSize:20}});
  if(path==='/api/scheduler')return route.fulfill({json:{sourceOptions:[],settings:{enabled:true,ownerName:'CI_OWNER',dayBudgetUsd:1,monthBudgetUsd:1,revision:1,cloudConnected:false,favoritesOnly:false,sourceIds:['hh'],intervalDays:7,priorities:{}},counts:{pending:1,running:0},runs:[],attempts:[],statistics:{sources:0,found:0,accepted:0,imported:0},spending:{dayUsd:0,monthUsd:0,byType:[],timezone:'Europe/Moscow'},lastSignalAt:null,nextSignalAt:null,cloudStatus:''}});
  return route.fulfill({status:503,json:{error:'CI unavailable'}});
 });
 await page.goto('/');
 const diagnostics=page.getByRole('tab',{name:'Диагностика'});
 if(isMobile)await diagnostics.tap();else await diagnostics.click();
 const button=page.getByRole('button',{name:'Продолжить сохранённый список'});
 await expect(button).toBeEnabled();
 if(isMobile)await button.tap();else await button.click();
 await expect.poll(()=>calls.length).toBe(1);
 await expect(page.getByRole('status').filter({hasText:'Сохранённый список продолжен.'})).toBeVisible();
 expect(calls[0].body.jobId).toBe(jobID);
 expect(calls[0].body.requestId).toMatch(/^[0-9a-f]{8}-[0-9a-f-]{27}$/);
 expect(Object.keys(calls[0].body).sort()).toEqual(['jobId','requestId']);
});

test('при приостановленном диспетчере продолжение недоступно',async({page,isMobile})=>{
 let posts=0;
 await page.route(url=>url.pathname.startsWith('/api/'),async route=>{
  const req=route.request(),path=new URL(req.url()).pathname;
  if(req.method()==='POST')posts++;
  if(path==='/api/scheduler/queue')return route.fulfill({json:{jobs:[{id:jobID,type:'scan_source',company_id:1,candidate_id:null,source_id:'hh',priority:20,status:'pending',run_after:0,attempts:0,max_attempts:4,last_error:'',companyName:'CI test'}],total:1,page:1,pageSize:20}});
  if(path==='/api/scheduler')return route.fulfill({json:{sourceOptions:[],settings:{enabled:false,ownerName:'CI_OWNER',dayBudgetUsd:0,monthBudgetUsd:0,revision:1,cloudConnected:false,favoritesOnly:false,sourceIds:['hh'],intervalDays:7,priorities:{}},counts:{pending:1,running:0},runs:[],attempts:[],statistics:{sources:0,found:0,accepted:0,imported:0},spending:{dayUsd:0,monthUsd:0,byType:[],timezone:'Europe/Moscow'},lastSignalAt:null,nextSignalAt:null,cloudStatus:''}});
  return route.fulfill({status:503,json:{error:'CI unavailable'}});
 });
 await page.goto('/');
 const diagnostics=page.getByRole('tab',{name:'Диагностика'});
 if(isMobile)await diagnostics.tap();else await diagnostics.click();
 await expect(page.getByRole('button',{name:'Продолжить сохранённый список'})).toBeDisabled();
 expect(posts).toBe(0);
});

test('возраст вакансий по умолчанию три дня и сохраняется режим всех вакансий',async({page})=>{
 const saved:Record<string,unknown>[]=[];
 const settings={enabled:false,ownerName:'CI_OWNER',dayBudgetUsd:0,monthBudgetUsd:0,revision:1,cloudConnected:false,favoritesOnly:false,sourceIds:['hh'],intervalDays:7,priorities:{}};
 await page.route(url=>url.pathname.startsWith('/api/'),async route=>{
  const req=route.request(),path=new URL(req.url()).pathname;
  if(path==='/api/scheduler/settings'&&req.method()==='PUT'){
   const body=req.postDataJSON();saved.push(body);
   return route.fulfill({json:{...settings,...body,revision:2}});
  }
  if(path==='/api/scheduler')return route.fulfill({json:{sourceOptions:[{id:'hh',name:'HH',supported:true,reason:null}],settings,counts:{pending:0,running:0},runs:[],attempts:[],statistics:{sources:0,found:0,accepted:0,imported:0},spending:{dayUsd:0,monthUsd:0,byType:[],timezone:'Europe/Moscow'},lastSignalAt:null,nextSignalAt:null,cloudStatus:''}});
  return route.fulfill({status:503,json:{error:'CI unavailable'}});
 });
 await page.goto('/');
 await page.getByRole('tab',{name:'Источники и правила'}).click();
 const age=page.getByLabel('Возраст вакансий');
 await expect(age).toHaveValue('3');
 await age.selectOption('0');
 await page.getByRole('button',{name:'Сохранить область и источники'}).click();
 await expect.poll(()=>saved.length).toBe(1);
 expect(saved[0].vacancyAgeDays).toBe(0);
 await expect(page.getByRole('status').filter({hasText:'Общие настройки парсинга сохранены.'})).toBeVisible();
});
