# 有条件的示例

仅当用户已确认 COMPLETED、balance、实付 pay_amount、不扣退款、UTC 日期等口径时可使用：

SELECT date_trunc('day', paid_at) AS day, sum(pay_amount) AS amount, count(DISTINCT user_id) AS customers
FROM analytics.payment_orders
WHERE status = $1 AND order_type = $2
GROUP BY 1 ORDER BY 1

parameters: ["COMPLETED", "balance"]
window_start: 2026-09-01T00:00:00Z
window_end: 2026-10-01T00:00:00Z

先调用 describe_dataset 确认字段。其他任务不能直接套用此口径。
