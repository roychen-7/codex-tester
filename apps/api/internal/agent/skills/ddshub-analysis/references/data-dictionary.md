# 分析数据字典

依据 sub2api v0.2.7，源码提交 7484192016807acf55c6ef4f2827d371eb61ec1c；synthetic 是人工构造的有限字段投影；live 是线上只读投影。先 describe_dataset 核对来源和可用字段，不假设两种来源字段完全相同。

- analytics.users：id、created_at、deleted_at。软删除是否排除由需求决定；线上 live 模式额外开放 email，可用于按邮箱查找客户并展示结果，模拟模式不提供 email。密钥始终不开放。
- analytics.payment_orders：id、user_id、amount、pay_amount、refund_amount、status、order_type、paid_at、created_at。user_id 关联 users.id。服务强制按 paid_at 过滤左闭右开窗口，无 paid_at 的订单不出现。
- analytics.usage_logs：id、user_id、model、input_tokens、output_tokens、total_cost、actual_cost、created_at。按 created_at 过滤窗口。行存在不自动证明请求成功。

充值金额可能指 amount、pay_amount 或扣退款后的金额，不可混用。测试样本的完成订单状态是 COMPLETED；这不意味着应默认把所有业务成功订单限定为该状态。balance 代表余额充值订单类型；订阅应另行确认。

prepare_query 的 window_start/window_end 必须是带时区的 RFC3339，最长 31 天；服务按 UTC 处理明确的时刻。若按北京时间分日，使用 date_trunc('day', timezone('Asia/Shanghai', paid_at))，边界相应转换。

SQL 必须显式使用 analytics.<table>，仅单条 SELECT；参数用 $1、$2，对应 parameters 字符串数组。支持 count/sum/avg/min/max/round/date_trunc/timezone/abs、基本连接和子查询；不支持 CTE、窗口函数、任意函数。不能执行写入、EXPLAIN ANALYZE 或直接查询原始表。

每次分析最多 8 次工具调用（含 Skill）、最多 3 次 execute_query；结果最多 200 行，正文最多约 256 KiB，模型预览可能更短。数字 ID、精确金额可能以字符串返回，应按列类型理解。

线上模式还可提供 describe_dataset 返回的注册来源、状态、余额、支付时间/退款时间、模型映射、缓存 token 和延迟等字段。只允许显式列出的字段，不可访问密码、IP、充值码、备注、syslog 或任意 public 表。
users 用于关联历史客户，不强制注册时间窗口；payment_orders 强制 paid_at 窗口，usage_logs 强制 created_at 窗口。退款按退款发生日、未支付订单等不在当前订单投影窗口的需求，应明确说明限制，不自行改换口径。

用户邮箱只用于本次内部客户查询和回答，不将邮箱放入公开联网搜索词。用户询问邮箱时先 describe_dataset 核对当前字段，不能沿用历史消息中“邮箱不开放”的旧限制。
