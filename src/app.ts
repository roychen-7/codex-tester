import { createServer, type IncomingMessage, type ServerResponse } from 'node:http';
import { ApiError, TaskStore } from './tasks.js';

function json(response: ServerResponse, status: number, value: unknown): void {
  response.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8' });
  response.end(JSON.stringify(value));
}
async function body(request: IncomingMessage): Promise<Record<string, unknown>> {
  if (request.headers['content-type']?.split(';')[0]?.trim().toLowerCase() !== 'application/json') {
    request.resume();
    throw new ApiError(415, 'UNSUPPORTED_MEDIA_TYPE', '请使用 application/json。');
  }
  const chunks: Buffer[] = [];
  let size = 0;
  for await (const chunk of request.iterator({ destroyOnReturn: false })) {
    const bytes = Buffer.from(chunk);
    size += bytes.length;
    if (size > 16 * 1024) {
      request.resume();
      throw new ApiError(413, 'BODY_TOO_LARGE', '请求体不能超过 16 KiB。');
    }
    chunks.push(bytes);
  }
  let value: unknown;
  try { value = JSON.parse(Buffer.concat(chunks).toString('utf8')); }
  catch { throw new ApiError(400, 'INVALID_JSON', '请求体必须是有效 JSON。'); }
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw new ApiError(400, 'INVALID_BODY', '请求体必须是 JSON 对象。');
  }
  return value as Record<string, unknown>;
}
export function createApp(store = new TaskStore()) {
  return createServer(async (request, response) => {
    try {
      const path = new URL(request.url ?? '/', 'http://localhost').pathname;
      const method = request.method;
      if (method === 'GET' && path === '/health') return json(response, 200, { data: { status: 'ok' } });
      if (method === 'GET' && path === '/tasks') return json(response, 200, { data: store.list() });
      if (method === 'POST' && path === '/tasks') {
        const input = await body(request);
        return json(response, 201, { data: store.create(input.title) });
      }
      const id = /^\/tasks\/([^/]+)$/.exec(path)?.[1];
      if (id && method === 'PATCH') {
        const input = await body(request);
        return json(response, 200, { data: store.update(id, input.status) });
      }
      if (id && method === 'DELETE') {
        store.delete(id);
        response.writeHead(204);
        response.end();
        return;
      }
      request.resume();
      throw new ApiError(404, 'NOT_FOUND', '接口不存在。');
    } catch (error) {
      if (error instanceof ApiError) return json(response, error.status, { error: { code: error.code, message: error.message } });
      console.error(error);
      json(response, 500, { error: { code: 'INTERNAL_ERROR', message: '服务器内部错误。' } });
    }
  });
}
