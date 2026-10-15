import { useEffect, useState } from "react";
import { Alert, Button, Typography, Card, Collapse, Space } from "antd";
import { agentAPI, errorText } from "./api";
import type { AnalysisOutcome } from "./api";
import ConclusionEvidence from "./ConclusionEvidence";
import EvidenceTable from "./EvidenceTable";

export default function MessageEvidence({
  conversation,
  message,
  onOpenConversation,
}: {
  conversation: string;
  message: string;
  onOpenConversation: (id: string) => void;
}) {
  const [data, setData] = useState<AnalysisOutcome | null>(null);
  const [error, setError] = useState("");
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setError("");
    setData(null);
    void agentAPI
      .evidence(conversation, message, controller.signal)
      .then((next) => {
        if (!controller.signal.aborted) setData(next);
      })
      .catch((reason: unknown) => {
        if (!controller.signal.aborted) setError(errorText(reason));
      });
    return () => controller.abort();
  }, [conversation, message, retry]);
  if (error)
    return (
      <Alert
        type="error"
        title={error}
        action={
          <Button onClick={() => setRetry((n) => n + 1)}>重试加载依据</Button>
        }
      />
    );
  if (!data)
    return (
      <Typography.Text type="secondary">正在加载回答依据…</Typography.Text>
    );
  return (
    <Space orientation="vertical" size="middle" style={{ width: "100%" }}>
      {(data.answer.conclusions?.length ?? 0) > 0 && (
        <ConclusionEvidence
          data={data}
          onOpenConversation={onOpenConversation}
        />
      )}
      {data.evidence.length > 0 && <EvidenceTable data={data} />}
      {(data.sources?.length ?? 0) > 0 && (
        <Card
          size="small"
          title={
            data.sources?.some((s) => s.kind === "web")
              ? "回答来源"
              : "历史来源"
          }
        >
          <Collapse
            items={data.sources?.map((source) => ({
              key: source.id,
              label:
                source.kind === "web"
                  ? `[${source.id}] ${source.title}`
                  : `[${source.id}] ${source.role === "user" ? "用户" : "Agent"} · ${new Date(source.created_at).toLocaleString()}`,
              children: (
                <>
                  <Typography.Paragraph style={{ whiteSpace: "pre-wrap" }}>
                    {source.content}
                  </Typography.Paragraph>
                  {source.truncated && (
                    <Typography.Paragraph type="secondary">
                      原文较长，这里仅保存引用片段。
                    </Typography.Paragraph>
                  )}
                  {source.kind === "history" ? (
                    <>
                      <Typography.Paragraph type="secondary">
                        会话 #{source.conversation_id} · 消息 #
                        {source.message_id}
                      </Typography.Paragraph>
                      <Button
                        onClick={() =>
                          onOpenConversation(source.conversation_id)
                        }
                      >
                        打开来源会话
                      </Button>
                    </>
                  ) : (
                    <>
                      <Typography.Paragraph type="secondary">
                        抓取时间：
                        {new Date(source.retrieved_at).toLocaleString()}
                        {source.published_at
                          ? `；网页标注日期：${source.published_at}`
                          : "；未提供发布时间"}
                      </Typography.Paragraph>
                      <Typography.Link
                        href={source.url}
                        target="_blank"
                        rel="noopener noreferrer"
                      >
                        打开原网页
                      </Typography.Link>
                    </>
                  )}
                </>
              ),
            }))}
          />
        </Card>
      )}
    </Space>
  );
}
