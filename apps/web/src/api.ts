export interface User {
  id: string;
  username: string;
  display_name: string;
}

export interface Conversation {
  id: string;
  title: string;
  created_at: string;
  updated_at: string;
  last_message_at: string | null;
}

export interface Message {
  id: string;
  conversation_id: string;
  request_id: string;
  role: "user" | "assistant";
  content: string;
  created_at: string;
}

export interface Page<T> {
  items: T[];
  next_cursor: string;
}

type Guard<T> = (value: unknown) => value is T;
const record = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null;
const id = (value: unknown): value is string =>
  typeof value === "string" && /^[1-9]\d*$/.test(value);
const date = (value: unknown): value is string =>
  typeof value === "string" && Number.isFinite(Date.parse(value));

const isUser: Guard<User> = (value): value is User =>
  record(value) &&
  id(value.id) &&
  typeof value.username === "string" &&
  typeof value.display_name === "string";

const isConversation: Guard<Conversation> = (value): value is Conversation =>
  record(value) &&
  id(value.id) &&
  typeof value.title === "string" &&
  date(value.created_at) &&
  date(value.updated_at) &&
  (value.last_message_at === null || date(value.last_message_at));

const isMessage: Guard<Message> = (value): value is Message =>
  record(value) &&
  id(value.id) &&
  id(value.conversation_id) &&
  typeof value.request_id === "string" &&
  (value.role === "user" || value.role === "assistant") &&
  typeof value.content === "string" &&
  date(value.created_at);

function page<T>(guard: Guard<T>): Guard<Page<T>> {
  return (value: unknown): value is Page<T> =>
    record(value) &&
    Array.isArray(value.items) &&
    value.items.every(guard) &&
    typeof value.next_cursor === "string";
}

export class APIError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}

async function request<T>(
  path: string,
  guard: Guard<T>,
  method = "GET",
  body?: unknown,
  signal?: AbortSignal,
): Promise<T> {
  const timeout = AbortSignal.timeout(15_000);
  const response = await fetch(`/api${path}`, {
    method,
    credentials: "same-origin",
    signal: signal ? AbortSignal.any([signal, timeout]) : timeout,
    headers: { "Content-Type": "application/json", "X-Agent-Request": "1" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const payload: unknown =
    response.status === 204 ? undefined : await response.json();
  if (!response.ok) {
    const message =
      record(payload) &&
      record(payload.error) &&
      typeof payload.error.message === "string"
        ? payload.error.message
        : "请求失败，请稍后重试。";
    throw new APIError(response.status, message);
  }
  const data = record(payload) ? payload.data : undefined;
  if (!guard(data)) throw new Error("服务器返回格式异常，请重试。");
  return data;
}

export const api = {
  me: (signal?: AbortSignal) =>
    request("/auth/me", isUser, "GET", undefined, signal),
  login: (username: string, password: string) =>
    request("/auth/login", isUser, "POST", { username, password }),
  logout: () =>
    request("/auth/logout", (v): v is undefined => v === undefined, "POST", {}),
  conversations: (cursor = "") =>
    request(
      `/conversations?cursor=${encodeURIComponent(cursor)}`,
      page(isConversation),
    ),
  createConversation: () =>
    request("/conversations", isConversation, "POST", { title: "新会话" }),
  conversation: (id: string, signal?: AbortSignal) =>
    request(`/conversations/${id}`, isConversation, "GET", undefined, signal),
  rename: (id: string, title: string) =>
    request(`/conversations/${id}`, isConversation, "PATCH", { title }),
  messages: (id: string, before = "", signal?: AbortSignal) =>
    request(
      `/conversations/${id}/messages?before=${encodeURIComponent(before)}`,
      page(isMessage),
      "GET",
      undefined,
      signal,
    ),
  send: (id: string, request_id: string, content: string) =>
    request(`/conversations/${id}/messages`, isMessage, "POST", {
      request_id,
      content,
    }),
};

export function errorText(error: unknown): string {
  if (error instanceof APIError) return error.message;
  if (error instanceof DOMException && error.name === "TimeoutError")
    return "请求超时，请重试。";
  if (error instanceof TypeError) return "连接失败，请检查网络后重试。";
  return error instanceof Error ? error.message : "暂时无法完成操作，请重试。";
}
