import { useEffect, useState } from 'react';

const statusLabels = {
  todo: '待开始',
  doing: '进行中',
  done: '已完成',
} as const;

interface Task {
  id: string;
  title: string;
  status: keyof typeof statusLabels;
}

type ListState =
  | { phase: 'loading' }
  | { phase: 'ready'; tasks: Task[] }
  | { phase: 'error'; message: string };

function isTask(value: unknown): value is Task {
  return typeof value === 'object' && value !== null
    && 'id' in value && typeof value.id === 'string'
    && 'title' in value && typeof value.title === 'string'
    && 'status' in value
    && (value.status === 'todo' || value.status === 'doing' || value.status === 'done');
}

export default function App() {
  const [state, setState] = useState<ListState>({ phase: 'loading' });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    const controller = new AbortController();

    async function loadTasks() {
      try {
        const response = await fetch('/api/tasks', { signal: controller.signal });
        if (!response.ok) throw new Error(`任务加载失败（HTTP ${response.status}）。`);

        const payload: unknown = await response.json();
        if (typeof payload !== 'object' || payload === null || !('data' in payload)
          || !Array.isArray(payload.data) || !payload.data.every(isTask)) {
          throw new Error('任务数据格式异常，请稍后重试。');
        }

        if (!controller.signal.aborted) setState({ phase: 'ready', tasks: payload.data });
      } catch (error) {
        if (controller.signal.aborted) return;
        setState({
          phase: 'error',
          message: error instanceof Error && !(error instanceof TypeError)
            ? error.message
            : '暂时无法连接服务，请检查后端是否启动后重试。',
        });
      }
    }

    void loadTasks();
    return () => controller.abort();
  }, [attempt]);

  function reload() {
    setState({ phase: 'loading' });
    setAttempt(current => current + 1);
  }

  return (
    <main className="app">
      <header className="page-header">
        <p className="eyebrow">CODEX / 开发练习场</p>
        <h1>把想法，变成完成。</h1>
        <p>从一个小任务开始，记录每一次进步。</p>
      </header>

      <section className="task-list" aria-labelledby="tasks-heading" aria-busy={state.phase === 'loading'}>
        <div className="section-heading">
          <h2 id="tasks-heading">我的任务</h2>
          <button type="button" onClick={reload} disabled={state.phase === 'loading'}>刷新列表</button>
        </div>

        <p role="status">
          {state.phase === 'loading' ? '正在加载任务…' : state.phase === 'ready' ? `共 ${state.tasks.length} 个任务` : ''}
        </p>

        {state.phase === 'error' && (
          <div className="error-message">
            <p role="alert">{state.message}</p>
            <button type="button" onClick={reload}>重新加载</button>
          </div>
        )}

        {state.phase === 'ready' && (state.tasks.length === 0 ? (
          <p className="empty-state">还没有任务，列表准备好了，等待你的第一个想法。</p>
        ) : (
          <ul>
            {state.tasks.map(task => (
              <li key={task.id} className="task-card">
                <span className="task-title">{task.title}</span>
                <span className={`task-status task-status--${task.status}`}>{statusLabels[task.status]}</span>
              </li>
            ))}
          </ul>
        ))}
      </section>

      <footer>任务保存在后端内存中，后端重启后会清空。</footer>
    </main>
  );
}
