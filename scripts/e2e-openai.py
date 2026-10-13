"""Loopback-only Responses API fixture. Never accepts or uses a real API key."""
import json
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
        if self.path != "/v1/responses" or self.headers.get("Authorization") != "Bearer e2e-only-key":
            self.send_error(403)
            return
        payload = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
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


ThreadingHTTPServer(("127.0.0.1", 13002), Handler).serve_forever()
