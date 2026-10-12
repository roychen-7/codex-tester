import { randomUUID } from 'node:crypto';

export type Status = 'todo' | 'doing' | 'done';
export interface Task { id: string; title: string; status: Status }
export class ApiError extends Error {
  constructor(public status: number, public code: string, message: string) { super(message); }
}
export class TaskStore {
  private tasks = new Map<string, Task>();
  list(): Task[] { return [...this.tasks.values()].map(task => ({ ...task })); }
  create(title: unknown): Task {
    if (typeof title !== 'string' || !title.trim() || title.trim().length > 120) {
      throw new ApiError(400, 'INVALID_TITLE', '标题应为 1–120 字的非空字符串。');
    }
    const task: Task = { id: randomUUID(), title: title.trim(), status: 'todo' };
    this.tasks.set(task.id, task);
    return { ...task };
  }
  update(id: string, status: unknown): Task {
    if (status !== 'todo' && status !== 'doing' && status !== 'done') {
      throw new ApiError(400, 'INVALID_STATUS', '状态应为 todo、doing 或 done。');
    }
    const task = this.tasks.get(id);
    if (!task) throw new ApiError(404, 'NOT_FOUND', '任务不存在。');
    const next: Task = { ...task, status };
    this.tasks.set(id, next);
    return { ...next };
  }
  delete(id: string): void {
    if (!this.tasks.delete(id)) throw new ApiError(404, 'NOT_FOUND', '任务不存在。');
  }
}
