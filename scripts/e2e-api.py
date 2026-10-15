"""Start an API against the isolated local test database, never the development database."""
from pathlib import Path
import os
import subprocess

root = Path(__file__).resolve().parents[1]
env = dict(os.environ)
source = os.environ.get("E2E_QUERY_SOURCE", "synthetic")
if source not in ("synthetic", "live"):
    raise SystemExit("Invalid test source")
env["AGENT_DATA_SOURCE"] = source
env.update({
    "APP_ENV": "local", "HOST": "127.0.0.1", "PORT": "13001",
    "MYSQL_HOST": "127.0.0.1", "MYSQL_PORT": os.environ.get("E2E_MYSQL_PORT", "13307"), "MYSQL_DATABASE": "agent_test",
    "MYSQL_USER": "agent", "MYSQL_PASSWORD": "agent-test-only", "MYSQL_TLS": "false",
    "COOKIE_SECURE": "false", "SESSION_TTL_HOURS": "168",
    "AGENT_MODE": os.environ.get("E2E_AGENT_MODE", "conversation"),
    "AGENT_MCP_URL": "http://127.0.0.1:13004/mcp", "AGENT_MCP_TOKEN": "e2e-query-token-not-for-production-only",
    "TAVILY_API_KEY":"e2e-tavily-only-key", "TAVILY_BASE_URL":"http://127.0.0.1:13002",
    "OPENAI_API_KEY": "e2e-only-key", "OPENAI_MODEL": "test-model",
    "OPENAI_BASE_URL": "http://127.0.0.1:13002/v1", "OPENAI_TIMEOUT_SECONDS": "10",
})
subprocess.run(["go", "-C", "apps/api", "build", "-o", "bin/manage", "./cmd/manage"], cwd=root, env=env, check=True)
subprocess.run(["go", "-C", "apps/api", "build", "-o", "bin/api", "./cmd/server"], cwd=root, env=env, check=True)
manage = str(root / "apps/api/bin/manage")
subprocess.run([manage, "migrate"], env=env, check=True)
for username in ("e2e_alice", "e2e_bob"):
    result = subprocess.run([manage, "create-user", username, username], input="e2e-test-password-123\n", text=True, capture_output=True, env=env)
    if result.returncode and "用户名已存在" not in result.stderr:
        raise SystemExit("无法初始化端到端测试账号：" + result.stderr)
os.execve(str(root / "apps/api/bin/api"), [str(root / "apps/api/bin/api")], env)
