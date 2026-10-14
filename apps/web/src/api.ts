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
  has_evidence: boolean;
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
  typeof value.has_evidence === "boolean" &&
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

export interface RequirementDetail {
  name: string;
  value: string;
  source: "user" | "history" | "suggestion";
  source_message_ids: string[];
}

export interface AgentTask {
  id: string;
  conversation_id: string;
  status: "clarifying" | "awaiting_confirmation" | "confirmed" | "cancelled";
  revision: number;
  requirement: { goal: string; details: RequirementDetail[] };
  missing_information: string[];
}

export interface LLMRun {
  id: string;
  conversation_id: string;
  status: "pending" | "running" | "succeeded" | "failed" | "cancelled";
  assistant_message_id: string | null;
  error_message: string | null;
}

export interface AnalysisRun extends LLMRun {
  task_id: string;
  task_revision: number;
  phase: string;
}

export interface AnalysisOutcome {
  answer: { reply: string; questions: string[]; result_ids: string[] };
  evidence: {
    sql: string;
    parameters: string[];
    purpose: string;
    window_start: string;
    window_end: string;
    result: {
      result_id: string;
      columns: string[];
      types: string[];
      rows: (string | number | boolean | null)[][];
      truncated: boolean;
      queried_at: string;
      elapsed_ms: number;
    };
  }[];
}

export interface AgentTurn extends Omit<LLMRun, "status"> {
  status: LLMRun["status"] | "waiting_for_user";
}

export interface AgentState {
  query_tools_enabled: boolean;
  turn_phase: string;
  mode: "conversation" | "legacy";
  turn: AgentTurn | null;
  analysis_enabled: boolean;
  analysis: AnalysisRun | null;
  enabled: boolean;
  task: AgentTask | null;
  run: LLMRun | null;
}

const isTask: Guard<AgentTask> = (value): value is AgentTask =>
  record(value) &&
  id(value.id) &&
  id(value.conversation_id) &&
  typeof value.status === "string" &&
  ["clarifying", "awaiting_confirmation", "confirmed", "cancelled"].includes(
    value.status,
  ) &&
  typeof value.revision === "number" &&
  Number.isSafeInteger(value.revision) &&
  value.revision > 0 &&
  record(value.requirement) &&
  typeof value.requirement.goal === "string" &&
  Array.isArray(value.requirement.details) &&
  value.requirement.details.every(
    (item: unknown) =>
      record(item) &&
      typeof item.name === "string" &&
      typeof item.value === "string" &&
      ["user", "history", "suggestion"].includes(String(item.source)) &&
      Array.isArray(item.source_message_ids) &&
      item.source_message_ids.every(id),
  ) &&
  Array.isArray(value.missing_information) &&
  value.missing_information.every((item: unknown) => typeof item === "string");

const isRun: Guard<LLMRun> = (value): value is LLMRun =>
  record(value) &&
  id(value.id) &&
  id(value.conversation_id) &&
  typeof value.status === "string" &&
  ["pending", "running", "succeeded", "failed", "cancelled"].includes(
    value.status,
  ) &&
  (value.assistant_message_id === null || id(value.assistant_message_id)) &&
  (value.error_message === null || typeof value.error_message === "string");

const isAnalysis: Guard<AnalysisRun> = (value): value is AnalysisRun =>
  isRun(value) &&
  record(value) &&
  id(value.task_id) &&
  typeof value.task_revision === "number" &&
  Number.isSafeInteger(value.task_revision) &&
  value.task_revision > 0 &&
  typeof value.phase === "string";
const strings = (value: unknown): value is string[] =>
  Array.isArray(value) && value.every((x: unknown) => typeof x === "string");
const isOutcome: Guard<AnalysisOutcome> = (value): value is AnalysisOutcome =>
  record(value) &&
  record(value.answer) &&
  typeof value.answer.reply === "string" &&
  strings(value.answer.questions) &&
  strings(value.answer.result_ids) &&
  Array.isArray(value.evidence) &&
  value.evidence.every(
    (e: unknown) =>
      record(e) &&
      typeof e.sql === "string" &&
      strings(e.parameters) &&
      typeof e.purpose === "string" &&
      date(e.window_start) &&
      date(e.window_end) &&
      record(e.result) &&
      typeof e.result.result_id === "string" &&
      strings(e.result.columns) &&
      strings(e.result.types) &&
      e.result.types.length === e.result.columns.length &&
      typeof e.result.truncated === "boolean" &&
      date(e.result.queried_at) &&
      typeof e.result.elapsed_ms === "number" &&
      Array.isArray(e.result.rows) &&
      e.result.rows.length <= 200 &&
      e.result.rows.every(
        (row: unknown) =>
          Array.isArray(row) &&
          row.length === (e.result as { columns: string[] }).columns.length &&
          row.every(
            (cell: unknown) =>
              cell === null ||
              typeof cell === "string" ||
              typeof cell === "boolean" ||
              (typeof cell === "number" && Number.isFinite(cell)),
          ),
      ),
  );

const isTurn: Guard<AgentTurn> = (value): value is AgentTurn =>
  record(value) &&
  id(value.id) &&
  id(value.conversation_id) &&
  typeof value.status === "string" &&
  [
    "pending",
    "running",
    "succeeded",
    "failed",
    "cancelled",
    "waiting_for_user",
  ].includes(value.status) &&
  (value.assistant_message_id === null || id(value.assistant_message_id)) &&
  (value.error_message === null || typeof value.error_message === "string");

const isAgentState: Guard<AgentState> = (value): value is AgentState =>
  record(value) &&
  typeof value.enabled === "boolean" &&
  typeof value.query_tools_enabled === "boolean" &&
  typeof value.turn_phase === "string" &&
  (value.mode === "conversation" || value.mode === "legacy") &&
  (value.turn === null || isTurn(value.turn)) &&
  typeof value.analysis_enabled === "boolean" &&
  (value.analysis === null || isAnalysis(value.analysis)) &&
  (value.task === null || isTask(value.task)) &&
  (value.run === null || isRun(value.run));

export const agentAPI = {
  evidence: (conversation: string, message: string, signal?: AbortSignal) =>
    request(
      `/conversations/${conversation}/messages/${message}/evidence`,
      isOutcome,
      "GET",
      undefined,
      signal,
    ),
  retryTurn: (conversation: string, run: string) =>
    request(
      `/conversations/${conversation}/turns/${run}/retry`,
      isTurn,
      "POST",
      {},
    ),
  cancelTurn: (conversation: string, run: string) =>
    request(
      `/conversations/${conversation}/turns/${run}/cancel`,
      (v): v is boolean => v === true,
      "POST",
      {},
    ),
  analyze: (
    conversation: string,
    task: string,
    revision: number,
    retry: boolean,
  ) =>
    request(
      `/conversations/${conversation}/tasks/${task}/analyze`,
      isAnalysis,
      "POST",
      { revision, retry },
    ),
  cancel: (conversation: string, run: string) =>
    request(
      `/conversations/${conversation}/analysis/${run}/cancel`,
      (v): v is boolean => v === true,
      "POST",
      {},
    ),
  result: (conversation: string, run: string, signal?: AbortSignal) =>
    request(
      `/conversations/${conversation}/analysis/${run}/result`,
      isOutcome,
      "GET",
      undefined,
      signal,
    ),
  state: (conversation: string, signal?: AbortSignal) =>
    request(
      `/conversations/${conversation}/agent`,
      isAgentState,
      "GET",
      undefined,
      signal,
    ),
  confirm: (conversation: string, task: string, revision: number) =>
    request(
      `/conversations/${conversation}/tasks/${task}/confirm`,
      isTask,
      "POST",
      { revision },
    ),
  retry: (conversation: string, run: string) =>
    request(
      `/conversations/${conversation}/runs/${run}/retry`,
      isRun,
      "POST",
      {},
    ),
};
