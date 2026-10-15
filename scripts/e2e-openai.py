"""Loopback-only Responses API fixture. Never accepts or uses a real API key."""
import json
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

attempts = {}


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def do_GET(self):
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b"ok")

    def do_POST(self):
        if self.path in ("/search", "/extract"):
            if self.headers.get("Authorization") != "Bearer e2e-tavily-only-key":
                self.send_error(403)
                return
            self.rfile.read(int(self.headers["Content-Length"]))
            result = {"results":[{"title":"模拟公开定价页", "url":"https://93.184.216.34/pricing", "content":"搜索摘要", "raw_content":"模拟网页正文：提供基础套餐，按月付费。仅为测试资料。"}]}
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps(result,ensure_ascii=False).encode())
            return
        if self.path != "/v1/responses" or self.headers.get("Authorization") != "Bearer e2e-only-key":
            self.send_error(403)
            return
        payload = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        if payload.get("text", {}).get("format", {}).get("name") == "conversation_answer":
            return self.conversation(payload)
        if isinstance(payload["input"], list):
            return self.analysis(payload)
        if payload.get("text", {}).get("format", {}).get("name") == "dialogue":
            return self.dialogue(payload)
        snapshot = json.loads(payload["input"])
        messages = snapshot["messages"]
        latest = next(message for message in reversed(messages) if message["role"] == "user")
        key = latest["id"]
        attempts[key] = attempts.get(key, 0) + 1
        if "测试生成失败" in latest["content"] and attempts[key] == 1:
            self.send_error(429)
            return
        ready = "2026年9月" in latest["content"]
        result = {
            "reply": "已整理需求，请点击确认需求。" if ready else "请补充分析的时间范围和留存口径。",
            "requirement": {
                "goal": "客户留存分析",
                "details": [{"name": "用户要求", "value": latest["content"], "source": "user", "source_message_ids": [key]}],
            },
            "missing_information": [] if ready else ["分析哪个时间范围？", "采用什么留存口径？"],
            "ready_for_confirmation": ready,
            "context_summary": "用户希望分析客户留存；以最近用户消息为准。",
        }
        data = json.dumps({
            "id": "resp_e2e_" + key,
            "status": "completed",
            "output": [{"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": json.dumps(result, ensure_ascii=False)}]}],
            "usage": {"input_tokens": 100, "output_tokens": 50},
        }).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(data)

    def conversation(self, payload):
        snapshot = json.loads(payload["input"][0]["content"])
        latest = next(x for x in reversed(snapshot["messages"]) if x["role"] == "user")
        text = latest["content"]
        outputs = [json.loads(x["output"]) for x in payload["input"] if x.get("type") == "function_call_output"]
        if "research-combine-test" in text:
            n = len(outputs)
            if n == 3:
                recovery_key = "native-recovery:" + latest["id"]
                if recovery_key not in attempts:
                    attempts[recovery_key] = 1
                    return self.respond([{"type":"web_search_call", "id":"native_untracked", "status":"completed"}])
                if payload.get("tool_choice") != {"type":"function", "name":"search_public_web"}:
                    self.send_error(500)
                    return
            steps = [("search_history", {"query":"历史测试约定", "before_id":""})]
            if n == 1:
                steps.append(("read_history", {"message_id":outputs[0]["items"][0]["message_id"]}))
            if n == 2:
                name, args = "load_skill", {"name":"web-research"}
            elif n == 3:
                name, args = "search_public_web", {"query":"public pricing", "domain":"", "time_range":""}
            elif n == 4:
                name, args = "read_webpage", {"url":"https://93.184.216.34/pricing"}
            elif n >= 5:
                source = next(s for s in outputs[1]["sources"] if "历史测试约定" in s["content"] and s["role"] == "user")
                web = outputs[-1]["sources"][0]
                answer = {"reply":"结合历史目标与公开网页给出建议（测试资料）。["+source["id"]+"] ["+web["id"]+"]", "questions":[], "result_ids":[], "source_ids":[source["id"],web["id"]], "conclusions":[{"text":"优先验证首次调用体验", "kind":"recommendation", "source_ids":[source["id"],web["id"]], "result_ids":[], "rationale":"历史约定优先留存，公开资料提供产品设计参考。", "limitations":"尚未验证接入体验是否导致用户流失。"}], "waiting_for_user":False,"context_summary":"历史和公开网页综合分析"}
                return self.respond([{"type":"message","role":"assistant","content":[{"type":"output_text","text":json.dumps(answer,ensure_ascii=False)}]}])
            else:
                name, args = steps[n]
            return self.respond([{"type":"function_call", "id":"fc_mix_"+str(n), "call_id":"mix_"+str(n), "name":name, "arguments":json.dumps(args)}])
        if "history-recall-test" in text:
            if not outputs:
                return self.respond([{"type":"function_call", "id":"fc_hs", "call_id":"hs", "name":"search_history", "arguments":json.dumps({"query":"历史测试约定", "before_id":""})}])
            if len(outputs) == 1:
                message_id = outputs[0]["items"][0]["message_id"]
                return self.respond([{"type":"function_call", "id":"fc_hr", "call_id":"hr", "name":"read_history", "arguments":json.dumps({"message_id":message_id})}])
            source = next(s for s in outputs[-1]["sources"] if "历史测试约定" in s["content"] and s["role"] == "user")
            answer = {"reply":"此前约定优先留存。[" + source["id"] + "]", "questions":[], "result_ids":[], "source_ids":[source["id"]], "waiting_for_user":False, "context_summary":"用户此前优先留存"}
            return self.respond([{"type":"message", "role":"assistant", "content":[{"type":"output_text", "text":json.dumps(answer,ensure_ascii=False)}]}])
        if "沿用刚才" in text:
            if not outputs:
                result_id = snapshot["previous_results"][0]["result_id"]
                return self.respond([{"type": "function_call", "id": "fc_read", "call_id": "call_read", "name": "read_result", "arguments": json.dumps({"result_id": result_id})}])
            result_id = outputs[-1]["result"]["data"]["result_id"]
            answer = {"reply": "沿用刚才的模拟查询：合计170，共3笔充值，平均每笔约56.67。", "questions": [], "result_ids": [result_id], "waiting_for_user": False, "context_summary": "按历史模拟查询计算平均充值金额。"}
            return self.respond([{"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": json.dumps(answer, ensure_ascii=False)}]}])
        if "conversation-query" in text:
            return self.analysis(payload)
        return self.dialogue({**payload, "input": json.dumps(snapshot), "conversation": True})

    def dialogue(self, payload):
        snapshot = json.loads(payload["input"])
        messages = snapshot["messages"]
        latest = next(x for x in reversed(messages) if x["role"] == "user")
        text = latest["content"]
        key = "dialogue:" + latest["id"]
        attempts[key] = attempts.get(key, 0) + 1
        if "dialogue-failure-once" in text and attempts[key] == 1:
            self.send_error(429)
            return
        if "dialogue-slow" in text:
            time.sleep(3)
        waiting = False
        if "Markdown 展示测试" in text:
            reply = """## 充值分析

模拟数据的总金额为 **170.00**，共 **3 笔**。

### 建议关注

- 比较新老客户的充值频次
- 观察后续使用与留存

> 当前仅为模拟数据，不能代表真实经营情况。

| 指标 | 数值 |
| --- | ---: |
| 总金额 | 170.00 |
| 订单数 | 3 |

使用 `pay_amount` 作为金额口径。

```sql
SELECT SUM(pay_amount) AS total, COUNT(*) AS orders FROM analytics.payment_orders WHERE status = 'COMPLETED' AND order_type = 'balance';
```

[说明链接](https://example.com) · [不安全链接](javascript:alert(1))

<img src=x onerror=alert(1)>
"""
        elif "想优化网站" in text:
            reply = "你目前更关心提高留存，还是增加收入？"
            waiting = True
        elif "历史测试约定" in text:
            reply = "已记录历史测试约定。"
        elif "优先留存" in text:
            if not any("想优化网站" in x["content"] for x in messages) or not snapshot["summary"]:
                self.send_error(500)
                return
            reply = "结合你之前的网站优化目标，我们先讨论留存：可以从新用户首次成功使用的体验入手。"
        else:
            reply = "留存关注用户是否持续回来使用，转化关注用户是否完成某个目标行为。"
        result = {"reply": reply, "waiting_for_user": waiting, "context_summary": "用户希望优化网站；优先讨论留存，尚未查询数据。"}
        if payload.get("conversation"):
            result.update({"questions": [reply] if waiting else [], "result_ids": []})
        self.respond([{"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": json.dumps(result, ensure_ascii=False)}]}])

    def analysis(self, payload):
        outputs = [json.loads(x["output"]) for x in payload["input"] if x.get("type") == "function_call_output"]
        n = len(outputs)
        original = payload["input"][0]["content"]
        if n == 0 and "analysis-failure-once" in original:
            attempts[original] = attempts.get(original, 0) + 1
            if attempts[original] == 1:
                self.send_error(429)
                return
        if n == 0 and "analysis-slow" in original:
            time.sleep(3)
        steps = [
            ("load_skill", {"name": "ddshub-analysis"}),
            ("read_skill_reference", {"name": "ddshub-analysis", "path": "references/data-dictionary.md"}),
            ("describe_dataset", {"table":"payment_orders"}),
            ("prepare_query", {"sql": "SELECT date_trunc('day', paid_at) AS day, SUM(pay_amount) AS total, COUNT(*) AS orders FROM analytics.payment_orders WHERE status = $1 AND order_type = $2 GROUP BY 1 ORDER BY 1", "parameters": ["COMPLETED", "balance"], "purpose": "模拟数据每日已完成余额充值，不扣退款", "window_start": "2026-09-01T00:00:00Z", "window_end": "2026-10-01T00:00:00Z"}),
        ]
        if n < len(steps):
            name, args = steps[n]
        elif n == 4:
            name, args = "execute_query", {"query_id": outputs[-1].get("query_id", "invalid")}
        else:
            result_id = outputs[-1].get("data", {}).get("result_id", "invalid")
            answer = {"reply": "模拟数据：9月1日充值150，9月2日充值20，合计170。", "questions": [], "result_ids": [result_id]}
            if payload.get("text", {}).get("format", {}).get("name") == "conversation_answer":
                waiting = "query-then-ask" in original
                answer.update({"waiting_for_user": waiting, "context_summary": "模拟数据充值总额170，共3笔。", "conclusions":[{"text":"模拟充值总额为170", "kind":"fact", "source_ids":[], "result_ids":[result_id], "rationale":"按日汇总的模拟结果相加为170。", "limitations":"仅为模拟数据，不代表真实经营情况。"}]})
                if waiting:
                    answer["reply"] += "接下来要按客户还是按订单分析？"
                    answer["questions"] = ["接下来要按客户还是按订单分析？"]
            if "data_source=live" in payload.get("instructions", ""):
                answer["reply"] = "线上模式隔离测试：合计170。"
                answer["context_summary"] = "线上模式隔离测试查询完成。"
                answer["conclusions"] = []
            self.respond([{"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": json.dumps(answer, ensure_ascii=False)}]}])
            return
        self.respond([{"type": "function_call", "id": "fc_"+str(n), "call_id": "call_"+str(n), "name": name, "arguments": json.dumps(args)}])

    def respond(self, output):
        data = json.dumps({"id": "resp_analysis", "status": "completed", "output": output, "usage": {"input_tokens": 20, "output_tokens": 20}}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(data)


ThreadingHTTPServer(("127.0.0.1", 13002), Handler).serve_forever()
