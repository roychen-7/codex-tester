import { createApp } from './app.js';
const port = Number(process.env.PORT ?? 3000);
if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error('PORT 必须是 1–65535 的整数。');
const server = createApp();
server.listen(port, '127.0.0.1', () => console.log(`API: http://127.0.0.1:${port}`));
for (const signal of ['SIGINT', 'SIGTERM'] as const) {
  process.once(signal, () => {
    server.close(() => process.exit(0));
    setTimeout(() => process.exit(1), 5000).unref();
  });
}
