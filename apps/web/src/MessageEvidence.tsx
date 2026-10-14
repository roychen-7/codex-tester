import { useEffect, useState } from "react";
import { Alert, Button, Typography } from "antd";
import { agentAPI, errorText } from "./api";
import type { AnalysisOutcome } from "./api";
import EvidenceTable from "./EvidenceTable";

export default function MessageEvidence({
  conversation,
  message,
}: {
  conversation: string;
  message: string;
}) {
  const [data, setData] = useState<AnalysisOutcome | null>(null);
  const [error, setError] = useState("");
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setError("");
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
      <Typography.Text type="secondary">正在加载查询依据…</Typography.Text>
    );
  return <EvidenceTable data={data} />;
}
