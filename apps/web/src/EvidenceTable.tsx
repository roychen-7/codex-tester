import { Alert, Card, Typography, Table, Collapse } from "antd";
import type { AnalysisOutcome } from "./api";
import styles from "./App.module.css";

export default function EvidenceTable({
  data,
  title = "分析依据",
}: {
  data: AnalysisOutcome;
  title?: string;
}) {
  return (
    <Card size="small" title={title}>
      {data.evidence.map((e) => (
        <div key={e.result.result_id} className={styles.evidenceItem}>
          <Typography.Paragraph>
            {e.result.data_source === "live" ? "线上只读数据" : "模拟数据"} ·{" "}
            {e.purpose} ·{" "}
            {data.answer.result_ids.includes(e.result.result_id)
              ? "已引用"
              : "查询记录"}
          </Typography.Paragraph>
          {e.result.truncated && (
            <Alert type="warning" title="查询结果已截断，表格并非完整数据。" />
          )}
          <Table
            size="small"
            pagination={false}
            scroll={{ x: true }}
            dataSource={e.result.rows.map((row, index) => ({
              key: index,
              row,
            }))}
            columns={e.result.columns.map((title, index) => ({
              title,
              key: index,
              render: (
                _: unknown,
                item: { row: (string | number | boolean | null)[] },
              ) =>
                item.row[index] === null ? "NULL" : String(item.row[index]),
            }))}
          />
          <Collapse
            items={[
              {
                key: "sql",
                label: "查看 SQL 与数据范围",
                children: (
                  <>
                    <Typography.Paragraph>
                      范围：{e.window_start} 至 {e.window_end}
                      （不含结束时间）
                    </Typography.Paragraph>
                    <Typography.Paragraph>
                      结果 ID：{e.result.result_id}；查询时间：
                      {e.result.queried_at}
                    </Typography.Paragraph>
                    <pre className={styles.querySql}>{e.sql}</pre>
                    {e.result.executed_sql &&
                      e.result.executed_sql !== e.sql && (
                        <>
                          <Typography.Paragraph>
                            实际执行的内部查询（含字段和时间限制）：
                          </Typography.Paragraph>
                          <pre className={styles.querySql}>
                            {e.result.executed_sql}
                          </pre>
                          <Typography.Paragraph>
                            执行参数：
                            {JSON.stringify(e.result.executed_parameters)}
                          </Typography.Paragraph>
                        </>
                      )}
                    <Typography.Paragraph>
                      参数：{JSON.stringify(e.parameters)}
                    </Typography.Paragraph>
                  </>
                ),
              },
            ]}
          />
        </div>
      ))}
      {data.evidence.length === 0 && (
        <Typography.Text>暂无查询依据，见上方回复。</Typography.Text>
      )}
    </Card>
  );
}
