"""MCP against synthetic test PostgreSQL only; no development credentials."""
from pathlib import Path
import os
import subprocess

root = Path(__file__).resolve().parents[1]
env = {k: v for k, v in os.environ.items() if not k.startswith("QUERY_POSTGRES_")}
source = os.environ.get("E2E_QUERY_SOURCE", "synthetic")
if source not in ("synthetic", "live"):
    raise SystemExit("Invalid test source")
env["QUERY_DATA_SOURCE"] = source
user, password = ("live_analyst", "live-test-only") if source == "live" else ("analyst", "analyst-test-only")
port = int(os.environ.get("E2E_POSTGRES_PORT", "15432"))
env.update({"APP_ENV": "local", "QUERY_MCP_ADDRESS": "127.0.0.1:13004", "QUERY_MCP_TOKEN": "e2e-query-token-not-for-production-only", "QUERY_POSTGRES_DSN": f"postgres://{user}:{password}@127.0.0.1:{port}/sub2api_test?sslmode=disable"})
subprocess.run(["go", "-C", "apps/api", "build", "-o", "bin/query-mcp-e2e", "./cmd/query-mcp"], cwd=root, env=env, check=True)
binary = str(root / "apps/api/bin/query-mcp-e2e")
os.execve(binary, [binary], env)
