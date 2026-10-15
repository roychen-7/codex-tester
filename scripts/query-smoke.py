"""Exercise only the fixed local synthetic MCP service; never reads .env."""
import json
import urllib.request

endpoint = "http://127.0.0.1:13003/mcp"
headers = {
    "Authorization": "Bearer query-test-only-token-not-for-production",
    "X-Agent-Subject": "smoke-user:smoke-run",
    "Content-Type": "application/json",
    "Accept": "application/json, text/event-stream",
    "MCP-Protocol-Version": "2025-06-18",
}


def rpc(method, params):
    payload = {"jsonrpc": "2.0", "id": 1, "method": method, "params": params}
    request = urllib.request.Request(endpoint, data=json.dumps(payload).encode(), headers=headers)
    with urllib.request.urlopen(request, timeout=30) as response:
        result = json.load(response)
    if "error" in result or result.get("result", {}).get("isError"):
        raise SystemExit(json.dumps(result, ensure_ascii=False))
    return result["result"]


rpc("initialize", {"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "synthetic-smoke", "version": "1"}})
print("工具:", ", ".join(item["name"] for item in rpc("tools/list", {})["tools"]))
prepared = rpc("tools/call", {"name": "prepare_query", "arguments": {
    "sql": "SELECT date_trunc('day', paid_at) AS day, sum(pay_amount) AS amount, count(DISTINCT user_id) AS customers FROM analytics.payment_orders WHERE status = $1 GROUP BY 1 ORDER BY 1",
    "parameters": ["COMPLETED"], "purpose": "模拟数据验收：九月每日充值金额和人数",
    "window_start": "2026-09-01T00:00:00Z", "window_end": "2026-10-01T00:00:00Z",
}})["structuredContent"]
result = rpc("tools/call", {"name": "execute_query", "arguments": {"query_id": prepared["query_id"]}})["structuredContent"]
assert len(result["rows"]) == 2 and not result["truncated"], result
assert result["rows"][0][1:] == ["150.00", "2"], result
assert result["rows"][1][1:] == ["20.00", "1"], result
print(json.dumps(result, ensure_ascii=False, indent=2))
print("模拟数据 MCP 验收通过")
