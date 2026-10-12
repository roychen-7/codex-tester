import test from 'node:test';
import assert from 'node:assert/strict';
import { once } from 'node:events';
import { createApp } from '../src/app.js';

test('任务 API 的 HTTP 行为', async t => {
  const server = createApp();
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  t.after(() => new Promise<void>((resolve, reject) => server.close(error => error ? reject(error) : resolve())));
  const address = server.address();
  assert.ok(address && typeof address !== 'string');
  const base = `http://127.0.0.1:${address.port}`;
  const send = (path: string, method = 'GET', input?: unknown) => fetch(base + path, {
    method,
    ...(input !== undefined ? { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(input) } : {}),
  });
  await t.test('健康检查', async () => {
    const result = await send('/health');
    assert.equal(result.status, 200);
    assert.deepEqual(await result.json(), { data: { status: 'ok' } });
  });
  await t.test('新增、查询、更新、删除以及资源不存在', async () => {
    assert.deepEqual(await (await send('/tasks')).json(), { data: [] });
    const created = await send('/tasks', 'POST', { title: '  接口测试  ' });
    assert.equal(created.status, 201);
    const { data: task } = await created.json();
    assert.equal(task.title, '接口测试');
    assert.equal(task.status, 'todo');
    assert.equal(typeof task.id, 'string');
    assert.deepEqual(await (await send('/tasks')).json(), { data: [task] });
    const updated = await send(`/tasks/${task.id}`, 'PATCH', { status: 'done' });
    assert.equal(updated.status, 200);
    assert.equal((await updated.json()).data.status, 'done');
    const removed = await send(`/tasks/${task.id}`, 'DELETE');
    assert.equal(removed.status, 204);
    assert.equal(await removed.text(), '');
    assert.deepEqual(await (await send('/tasks')).json(), { data: [] });
    assert.equal((await send(`/tasks/${task.id}`, 'DELETE')).status, 404);
    assert.equal((await send(`/tasks/${task.id}`, 'PATCH', { status: 'doing' })).status, 404);
  });
  await t.test('无效字段及 JSON 结构不写入数据', async () => {
    for (const input of [{}, { title: ' ' }, { title: 42 }, { title: 'x'.repeat(121) }, [], null]) {
      const result = await send('/tasks', 'POST', input);
      assert.equal(result.status, 400);
      assert.equal(typeof (await result.json()).error.code, 'string');
    }
    assert.equal((await send('/tasks/missing', 'PATCH', { status: 'invalid' })).status, 400);
    assert.deepEqual(await (await send('/tasks')).json(), { data: [] });
  });
  await t.test('非法 JSON、媒体类型和超大请求', async () => {
    const malformed = await fetch(base + '/tasks', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{' });
    assert.equal(malformed.status, 400);
    assert.equal((await malformed.json()).error.code, 'INVALID_JSON');
    assert.equal((await send('/tasks', 'POST')).status, 415);
    const large = await send('/tasks', 'POST', { title: 'x'.repeat(20000) });
    assert.equal(large.status, 413);
    assert.equal((await large.json()).error.code, 'BODY_TOO_LARGE');
    assert.equal((await send('/health')).status, 200);
    assert.equal((await send('/missing')).status, 404);
  });
});
