import {test, expect} from '../browser/fixtures.js';

// Browser → Go → одноразовая MariaDB. Внешние провайдеры не используются.
test('Канал: валидация, создание, изменение комментария и права', async ({page, activate}, info) => {
  const password = process.env.ITMAP_SMOKE_PASSWORD;
  if (!password || process.env.ITMAP_CHANNEL_BROWSER_WRITES !== '1') throw new Error('Требуется изолированная CI-БД');
  const name = 'CI Browser Channel ' + info.project.name;
  const comment = 'CI browser comment ' + info.project.name;
  const login = async (user: string) => {
    await page.goto('/');
    await page.getByLabel('Логин', {exact:true}).fill(user);
    await page.getByLabel('Пароль', {exact:true}).fill(password);
    await activate(page.getByRole('button', {name:'Открыть карту', exact:true}));
    await expect(page.getByRole('navigation', {name:'Основные разделы'})).toBeVisible();
  };
  const navigate = async () => {
    await activate(page.getByRole('button', {name:'Список', exact:true}));
    await activate(page.getByRole('group', {name:'Подразделы: Список'}).getByRole('button', {name:'Каналы', exact:true}));
    await expect(page.locator('.company-list-shell')).toContainText('Список каналов');
  };
  await login('eugene');
  await navigate();
  await activate(page.getByRole('button', {name:'Добавить', exact:true}));
  const form=page.locator('.channel-create-card');
  await expect(form).toBeVisible();
  await form.getByLabel('Название', {exact:true}).fill('a'.repeat(161));
  await form.getByLabel('Ссылка на канал', {exact:true}).fill('https://t.me/itmap_ci_' + info.project.name);
  const invalid=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/channels' && r.request().method()==='POST');
  await activate(form.getByRole('button', {name:'Добавить канал'}));
  expect((await invalid).status()).toBe(400);
  await expect(form.getByRole('alert')).toContainText('Название канала не должно превышать 160 символов');
  await expect(form.getByLabel('Название', {exact:true})).toHaveValue('a'.repeat(161));
  await form.getByLabel('Название', {exact:true}).fill(name);

  const createdResponse=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/channels' && r.request().method()==='POST');
  await activate(form.getByRole('button', {name:'Добавить канал'}));
  const created=await createdResponse;
  expect(created.status()).toBe(201);
  const body=await created.json();
  expect(body.ok).toBe(true);
  expect(body.channelId).toBeGreaterThan(0);
  await expect(page.locator('.channel-list-table')).toContainText(name);
  await page.reload();
  await navigate();
  const row=page.locator('.channel-list-table tbody tr').filter({hasText:name});
  await expect(row).toHaveCount(1);
  await activate(row.getByRole('button',{name, exact:true}));
  const drawer=page.getByRole('dialog',{name, exact:true});
  await expect(drawer).toBeVisible();
  const editor=drawer.locator('.channel-comment-form');
  await editor.getByRole('textbox',{name:'Комментарий'}).fill(comment);
  const updated=page.waitForResponse(r=>new URL(r.url()).pathname=== '/api/channels/'+body.channelId && r.request().method()==='PUT');
  await activate(editor.getByRole('button',{name:'Сохранить комментарий'}));
  expect((await updated).status()).toBe(200);
  await page.reload();
  await navigate();
  await expect(page.locator('.channel-list-table tbody tr').filter({hasText:name})).toContainText(comment);
  const api=await page.request.get('/api/channels');
  expect(api.status()).toBe(200);
  const persisted=(await api.json()).channels.find((c:{id:number})=>c.id===body.channelId);
  expect(persisted).toMatchObject({id:body.channelId,name,comment,url:'https://t.me/itmap_ci_'+info.project.name});

  await page.goto('/logout');
  await login('demo');
  await navigate();
  await expect(page.locator('.company-list-shell').getByRole('button',{name:'Добавить', exact:true})).toHaveCount(0);
  const denied=await page.request.post('/api/channels',{data:{name:'CI forbidden',url:'https://t.me/itmap_forbidden'}});
  expect(denied.status()).toBe(403);
  const read=await page.request.get('/api/channels');
  expect(read.status()).toBe(200);
  expect((await read.json()).channels.some((c:{id:number})=>c.id===body.channelId)).toBe(true);
});
