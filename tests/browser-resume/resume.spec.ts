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
 const button=page.getByRole('button',{name:'Продолжить HH'});
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
 await expect(page.getByRole('button',{name:'Продолжить HH'})).toBeDisabled();
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

test('роли HH выбираются независимо для каждого направления',async({page})=>{
 const saved:Record<string,unknown>[]=[];
 const generation='12345678-1234-4234-8234-123456789abd';
 await page.route(url=>url.pathname.startsWith('/api/'),async route=>{
  const req=route.request(),path=new URL(req.url()).pathname;
  if(path==='/api/hh/role-settings'&&req.method()==='PUT'){const body=req.postDataJSON();saved.push(body);return route.fulfill({json:{...body,name:body.roleId===1?'IT Manager':'Журналист',revision:body.revision+1}});}
  if(path==='/api/hh/role-settings')return route.fulfill({json:{generation,roles:[{roleId:1,name:'IT Manager',revision:0,roleIds:[]},{roleId:2,name:'Журналист',revision:0,roleIds:[]}]}});
  if(path==='/api/hh/professional-roles')return route.fulfill({json:{categories:[{id:'1',name:'ИТ',roles:[{id:'36',name:'CIO'},{id:'107',name:'Руководитель проектов'}]},{id:'2',name:'Другое',roles:[{id:'107',name:'Руководитель проектов'}]}]}});
  if(path==='/api/scheduler')return route.fulfill({json:{sourceOptions:[],settings:{enabled:false,ownerName:'CI_OWNER',dayBudgetUsd:0,monthBudgetUsd:0,revision:1,cloudConnected:false,favoritesOnly:false,sourceIds:['hh'],intervalDays:7,priorities:{}},counts:{pending:0,running:0},runs:[],attempts:[],statistics:{sources:0,found:0,accepted:0,imported:0},spending:{dayUsd:0,monthUsd:0,byType:[],timezone:'Europe/Moscow'},lastSignalAt:null,nextSignalAt:null,cloudStatus:''}});
  return route.fulfill({status:503,json:{error:'CI unavailable'}});
 });
 await page.goto('/');await page.getByRole('tab',{name:'Источники и правила'}).click();
 const it=page.locator('article').filter({has:page.getByRole('heading',{name:'IT Manager'})});
 const news=page.locator('article').filter({has:page.getByRole('heading',{name:'Журналист'})});
 await it.getByText('ИТ',{exact:true}).click();await it.getByLabel('Руководитель проектов').first().check();
 await it.getByText('Другое',{exact:true}).click();await expect(it.getByLabel('Руководитель проектов').last()).toBeChecked();
 await news.getByText('ИТ',{exact:true}).click();await expect(news.getByLabel('Руководитель проектов').first()).not.toBeChecked();
 await it.getByRole('button',{name:'Сохранить роли HH'}).click();await expect.poll(()=>saved.length).toBe(1);
 expect(saved[0]).toMatchObject({generation,roleId:1,revision:0,roleIds:['107']});
 await news.getByLabel('CIO').check();await news.getByRole('button',{name:'Сохранить роли HH'}).click();await expect.poll(()=>saved.length).toBe(2);
 expect(saved[1]).toMatchObject({generation,roleId:2,revision:0,roleIds:['36']});
});
