import { useEffect, useRef, useState } from "react";
import { Alert, Button, Card, Space, Tag, Typography } from "antd";
import { agentAPI, api, APIError, errorText } from "./api";
import type { AgentState, Message } from "./api";
import styles from "./App.module.css";

const sourceLabels = {
  user: "用户说明",
  history: "历史参考",
  suggestion: "模型建议",
};

export default function AgentPanel({
  conversation,
  refresh,
  onBusy,
  onMessages,
  onError,
}: {
  conversation: string;
  refresh: number;
  onBusy: (conversation: string, busy: boolean) => void;
  onMessages: (conversation: string, messages: Message[]) => void;
  onError: (reason: unknown) => void;
}) {
  const [state, setState] = useState<AgentState | null>(null);
  const [error, setError] = useState("");
  const [actionBusy, setActionBusy] = useState(false);
  const [reload, setReload] = useState(0);
  const seenAnswer = useRef("");
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      try {
        const next = await agentAPI.state(conversation, controller.signal);
        if (controller.signal.aborted) return;
        setState(next);
        setError("");
        onBusy(
          conversation,
          next.run?.status === "pending" || next.run?.status === "running",
        );
        const answer = next.run?.assistant_message_id;
        if (answer && answer !== seenAnswer.current) {
          const page = await api.messages(conversation, "", controller.signal);
          if (controller.signal.aborted) return;
          onMessages(conversation, page.items);
          seenAnswer.current = answer;
        }
      } catch (reason) {
        if (controller.signal.aborted) return;
        if (reason instanceof APIError && reason.status === 401) {
          onError(reason);
          return;
        }
        setError(errorText(reason));
      } finally {
        if (!controller.signal.aborted)
          timer = setTimeout(() => void poll(), 1500);
      }
    };
    void poll();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [conversation, refresh, reload, onBusy, onMessages, onError]);

  async function act(kind: "confirm" | "retry") {
    if (!state) return;
    setActionBusy(true);
    setError("");
    try {
      if (kind === "confirm" && state.task) {
        await agentAPI.confirm(
          conversation,
          state.task.id,
          state.task.revision,
        );
      } else if (kind === "retry" && state.run) {
        await agentAPI.retry(conversation, state.run.id);
        if (mounted.current) onBusy(conversation, true);
      }
      if (mounted.current) setReload((value) => value + 1);
    } catch (reason) {
      if (!mounted.current) return;
      if (reason instanceof APIError && reason.status === 401) onError(reason);
      else setError(errorText(reason));
    } finally {
      if (mounted.current) setActionBusy(false);
    }
  }

  const working =
    state?.run?.status === "pending" || state?.run?.status === "running";
  const task = state?.task;
  return (
    <div className={styles.agentPanel} aria-label="Agent 需求状态">
      {error && <Alert type="error" title={error} showIcon />}
      {!state && !error && (
        <Typography.Text type="secondary">正在加载 Agent 状态…</Typography.Text>
      )}
      {state && !state.enabled && (
        <Alert type="info" title="尚未配置模型，当前仅保存消息。" />
      )}
      {working && (
        <Alert
          type="info"
          title="Agent 正在理解你的问题…"
          description="可以切换会话或刷新页面，回复会保存到当前会话。"
          showIcon
        />
      )}
      {state?.run?.status === "failed" && (
        <Alert
          type="error"
          title={state.run.error_message ?? "生成失败，请重试"}
          action={
            <Button
              loading={actionBusy}
              disabled={!state.enabled}
              onClick={() => void act("retry")}
            >
              重试生成
            </Button>
          }
        />
      )}
      {task && task.requirement.goal && (
        <Card
          size="small"
          title="需求摘要"
          extra={
            <Tag color={task.status === "confirmed" ? "green" : "blue"}>
              {task.status === "confirmed"
                ? "已确认"
                : task.status === "awaiting_confirmation"
                  ? "待你确认"
                  : "澄清中"}
            </Tag>
          }
        >
          <Typography.Paragraph>{task.requirement.goal}</Typography.Paragraph>
          {task.requirement.details.map((item, index) => (
            <p key={index} className={styles.requirementDetail}>
              <strong>{item.name}：</strong>
              {item.value} <Tag>{sourceLabels[item.source]}</Tag>
            </p>
          ))}
          {task.missing_information.length > 0 && (
            <div>
              <strong>还需要明确：</strong>
              <ul>
                {task.missing_information.map((item, index) => (
                  <li key={index}>{item}</li>
                ))}
              </ul>
            </div>
          )}
          {task.status === "awaiting_confirmation" && (
            <Space wrap>
              <Button
                type="primary"
                loading={actionBusy}
                disabled={working}
                onClick={() => void act("confirm")}
              >
                确认需求
              </Button>
              <Typography.Text type="secondary">
                需要修改？直接在下方补充说明。
              </Typography.Text>
            </Space>
          )}
          {task.status === "confirmed" && (
            <Typography.Text type="secondary">
              需求已确认。业务数据查询尚未接入；继续补充会重新进入澄清。
            </Typography.Text>
          )}
        </Card>
      )}
    </div>
  );
}
