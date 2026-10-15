import { useEffect, useRef, useState } from "react";
import { Alert, Button, Card, Space, Tag, Typography } from "antd";
import { agentAPI, api, APIError, errorText } from "./api";
import type { AgentState, Message, AnalysisOutcome } from "./api";
import EvidenceTable from "./EvidenceTable";
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
  const [outcome, setOutcome] = useState<{
    run: string;
    data: AnalysisOutcome;
  } | null>(null);
  const loadedResult = useRef("");
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
          next.mode === "legacy" &&
            (next.run?.status === "pending" || next.run?.status === "running"),
        );
        const answer = [
          next.run?.assistant_message_id,
          next.turn?.assistant_message_id,
          next.analysis?.assistant_message_id,
        ]
          .filter(Boolean)
          .join(":");
        if (answer && answer !== seenAnswer.current) {
          const page = await api.messages(conversation, "", controller.signal);
          if (controller.signal.aborted) return;
          onMessages(conversation, page.items);
          seenAnswer.current = answer;
        }
        if (
          next.mode === "legacy" &&
          next.analysis?.status === "succeeded" &&
          loadedResult.current !== next.analysis.id
        ) {
          const data = await agentAPI.result(
            conversation,
            next.analysis.id,
            controller.signal,
          );
          if (controller.signal.aborted) return;
          setOutcome({ run: next.analysis.id, data });
          loadedResult.current = next.analysis.id;
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

  async function act(
    kind:
      | "confirm"
      | "retry"
      | "analyze"
      | "cancel"
      | "retryTurn"
      | "cancelTurn",
  ) {
    if (!state) return;
    setActionBusy(true);
    setError("");
    try {
      if (kind === "retryTurn" && state.turn) {
        await agentAPI.retryTurn(conversation, state.turn.id);
      } else if (kind === "cancelTurn" && state.turn) {
        await agentAPI.cancelTurn(conversation, state.turn.id);
      } else if (kind === "analyze" && state.task) {
        await agentAPI.analyze(
          conversation,
          state.task.id,
          state.task.revision,
          state.analysis?.task_revision === state.task.revision &&
            ["failed", "cancelled"].includes(state.analysis.status),
        );
      } else if (kind === "cancel" && state.analysis) {
        await agentAPI.cancel(conversation, state.analysis.id);
      } else if (kind === "confirm" && state.task) {
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
  const analysis = state?.analysis;
  const analyzing =
    analysis?.status === "pending" || analysis?.status === "running";
  const currentAnalysis =
    analysis &&
    task &&
    analysis.task_id === task.id &&
    analysis.task_revision === task.revision;
  const phases: Record<string, string> = {
    read_result: "读取历史查询依据",
    search_history: "检索历史对话",
    read_history: "阅读历史消息",
    search_public_web: "搜索网上资料",
    read_webpage: "阅读网页正文",
    queued: "等待分析",
    connecting: "连接数据服务",
    responses: "思考并整理结果",
    load_skill: "读取分析指引",
    read_skill_reference: "读取数据字典",
    describe_dataset: "查看数据结构",
    prepare_query: "检查查询",
    execute_query: "查询数据",
  };
  if (state?.mode === "conversation") {
    const turn = state.turn;
    const busy = turn?.status === "pending" || turn?.status === "running";
    return (
      <div className={styles.agentPanel} aria-label="Agent 对话状态">
        {error && <Alert type="error" title={error} showIcon />}
        {!state.enabled && (
          <Alert type="info" title="尚未配置模型，当前仅保存消息。" />
        )}
        <Typography.Text type="secondary">
          {state.query_tools_enabled
            ? "可按需查询已配置的数据源，必要时会向你追问。"
            : "当前可进行连续讨论；数据查询尚未配置。"}
        </Typography.Text>
        {busy && (
          <Alert
            type="info"
            showIcon
            title={phases[state.turn_phase] ?? "Agent 正在回复…"}
            description="可以继续补充；新消息会停止当前生成，结合最新内容重新回复。"
            action={
              <Button
                loading={actionBusy}
                onClick={() => void act("cancelTurn")}
              >
                停止回复
              </Button>
            }
          />
        )}
        {turn?.status === "waiting_for_user" && (
          <Typography.Text type="secondary">
            等待你的补充，可以直接在下方回复。
          </Typography.Text>
        )}
        {(turn?.status === "failed" || turn?.status === "cancelled") && (
          <Alert
            type={turn.status === "failed" ? "error" : "info"}
            title={
              turn.status === "failed"
                ? (turn.error_message ?? "回复失败")
                : "回复已停止"
            }
            action={
              <Button
                disabled={!state.enabled}
                loading={actionBusy}
                onClick={() => void act("retryTurn")}
              >
                重试回复
              </Button>
            }
          />
        )}
      </div>
    );
  }

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
                {state?.analysis_enabled ? "确认并分析" : "确认需求"}
              </Button>
              <Typography.Text type="secondary">
                需要修改？直接在下方补充说明。
              </Typography.Text>
            </Space>
          )}
          {task.status === "confirmed" && (
            <Typography.Text type="secondary">
              需求已确认。继续补充会取消正在进行的分析，并重新澄清。
            </Typography.Text>
          )}
        </Card>
      )}
      {state?.analysis_enabled &&
        task?.status === "confirmed" &&
        !working &&
        (!currentAnalysis ||
          ["failed", "cancelled"].includes(analysis.status)) && (
          <Button loading={actionBusy} onClick={() => void act("analyze")}>
            {currentAnalysis ? "重试分析" : "开始分析"}
          </Button>
        )}
      {analyzing && (
        <Alert
          type="info"
          showIcon
          title={phases[analysis.phase] ?? "正在分析"}
          description="数据来源见查询依据。可以刷新页面；补充需求会取消本次分析。"
          action={
            <Button loading={actionBusy} onClick={() => void act("cancel")}>
              取消分析
            </Button>
          }
        />
      )}
      {currentAnalysis && analysis.status === "failed" && (
        <Alert type="error" title={analysis.error_message ?? "分析失败"} />
      )}
      {currentAnalysis && analysis.status === "cancelled" && (
        <Alert type="info" title="分析已取消" />
      )}
      {outcome &&
        analysis?.id === outcome.run &&
        analysis.status === "succeeded" && (
          <EvidenceTable
            data={outcome.data}
            title={currentAnalysis ? "分析依据" : "之前需求的分析依据"}
          />
        )}
    </div>
  );
}
