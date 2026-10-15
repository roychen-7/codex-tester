import { useState } from "react";
import { Button, Card, Modal, Space, Tag, Typography } from "antd";
import type { AnalysisOutcome, AnswerSource } from "./api";
import styles from "./ConclusionEvidence.module.css";
import EvidenceTable from "./EvidenceTable";

const kinds = {
  fact: "已核实事实",
  inference: "综合推断",
  recommendation: "待验证建议",
};

export default function ConclusionEvidence({
  data,
  onOpenConversation,
}: {
  data: AnalysisOutcome;
  onOpenConversation: (id: string) => void;
}) {
  const [selected, setSelected] = useState<{
    source?: AnswerSource;
    resultID?: string;
    label: string;
  } | null>(null);
  const sourceLabel = (source: AnswerSource) => {
    const index =
      (data.sources ?? [])
        .filter((item) => item.kind === source.kind)
        .findIndex((item) => item.id === source.id) + 1;
    return `${source.kind === "history" ? "历史 H" : "网页 W"}${index}`;
  };
  const selectedQuery = data.evidence.filter(
    (item) => item.result.result_id === selected?.resultID,
  );
  return (
    <>
      <Space orientation="vertical" size="middle" className={styles.list}>
        {data.answer.conclusions?.map((conclusion, index) => (
          <Card
            key={index}
            size="small"
            title={`结论 ${index + 1}`}
            extra={<Tag>{kinds[conclusion.kind]}</Tag>}
          >
            <Typography.Paragraph strong>
              {conclusion.text}
            </Typography.Paragraph>
            <Space wrap>
              <Typography.Text type="secondary">依据：</Typography.Text>
              {conclusion.source_ids.map((id) => {
                const source = data.sources?.find((item) => item.id === id);
                if (!source) return null;
                const label = sourceLabel(source);
                return (
                  <Button
                    key={id}
                    size="small"
                    onClick={() => setSelected({ source, label })}
                  >
                    {label}
                  </Button>
                );
              })}
              {conclusion.result_ids.map((id) => {
                const index = data.evidence.findIndex(
                  (item) => item.result.result_id === id,
                );
                if (index < 0) return null;
                const label = `查询 D${index + 1}`;
                return (
                  <Button
                    key={id}
                    size="small"
                    onClick={() => setSelected({ resultID: id, label })}
                  >
                    {label}
                  </Button>
                );
              })}
            </Space>
            <Typography.Paragraph className={styles.rationale}>
              <Typography.Text strong>依据说明：</Typography.Text>
              {conclusion.rationale}
            </Typography.Paragraph>
            {conclusion.limitations && (
              <Typography.Paragraph type="secondary">
                <Typography.Text strong>限制与待验证：</Typography.Text>
                {conclusion.limitations}
              </Typography.Paragraph>
            )}
          </Card>
        ))}
      </Space>
      <Modal
        destroyOnHidden
        open={selected !== null}
        title={selected?.label}
        footer={null}
        onCancel={() => setSelected(null)}
      >
        {selected?.source && (
          <>
            <Typography.Paragraph strong>
              {selected.source.title}
            </Typography.Paragraph>
            <Typography.Paragraph className={styles.excerpt}>
              {selected.source.content}
            </Typography.Paragraph>
            {selected.source.truncated && (
              <Typography.Paragraph type="secondary">
                原文较长，此处为截断片段。
              </Typography.Paragraph>
            )}
            {selected.source.kind === "web" ? (
              <>
                <Typography.Paragraph type="secondary">
                  抓取时间：
                  {new Date(selected.source.retrieved_at).toLocaleString()}
                </Typography.Paragraph>
                <Typography.Link
                  href={selected.source.url}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  打开原网页
                </Typography.Link>
              </>
            ) : (
              <>
                <Typography.Paragraph type="secondary">
                  {selected.source.role === "user" ? "用户" : "Agent"} ·{" "}
                  {new Date(selected.source.created_at).toLocaleString()} · 消息
                  #{selected.source.message_id}
                </Typography.Paragraph>
                <Button
                  onClick={() => {
                    if (selected.source?.kind === "history") {
                      onOpenConversation(selected.source.conversation_id);
                      setSelected(null);
                    }
                  }}
                >
                  打开来源会话
                </Button>
              </>
            )}
          </>
        )}
        {selectedQuery.length > 0 && (
          <EvidenceTable
            data={{ answer: data.answer, evidence: selectedQuery }}
          />
        )}
      </Modal>
    </>
  );
}
