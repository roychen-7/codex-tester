# 模拟分析数据字典

依据 sub2api v0.2.7，源码提交 7484192016807acf55c6ef4f2827d371eb61ec1c；这是人工构造的有限字段投影，非生产数据。

- analytics.users：id、created_at、deleted_at。软删除是否排除由需求决定，不提供 email、密钥。
- analytics.payment_orders：id、user_id、amount、pay_amount、refund_amount、status、order_type、paid_at、created_at。user_id 关联 users.id。视图强制按 paid_at 过滤左闭右开窗口，无 paid_at 的订单不出现。
- analytics.usage_logs：id、user_id、model、input_tokens、output_tokens、total_cost、actual_cost、created_at。按 created_at 过滤窗口。行存在不自动证明请求成功。

充值金额可能指 amount、pay_amount 或扣退款后的金额，不可混用。测试样本的完成订单状态是 COMPLETED；这不意味着应默认把所有业务成功订单限定为该状态。balance 代表余额充值订单类型；订阅应另行确认。

prepare_query 的 window_start/window_end 必须是带时区的 RFC3339，最长 31 天；服务按 UTC 处理明确的时刻。若按北京时间分日，使用 date_trunc('day', timezone('Asia/Shanghai', paid_at))，边界相应转换。

SQL 必须显式使用 analytics.<view>，仅单条 SELECT；参数用 $1、$2，对应 parameters 字符串数组。支持 count/sum/avg/min/max/round/date_trunc/timezone/abs、基本连接和子查询；不支持 CTE、窗口函数、任意函数。不能执行写入、EXPLAIN ANALYZE 或直接查询原始表。

每次分析最多 8 次工具调用（含 Skill）、最多 3 次 execute_query；结果最多 200 行，正文最多约 256 KiB，模型预览可能更短。数字 ID、精确金额可能以字符串返回，应按列类型理解。
