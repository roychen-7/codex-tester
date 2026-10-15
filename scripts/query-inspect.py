"""Read only catalog metadata using root .env; never prints connection credentials."""
import os
from pathlib import Path
import subprocess


root = Path(__file__).resolve().parent.parent
result = subprocess.run(
    ["docker", "compose", "config", "--environment"],
    cwd=root,
    capture_output=True,
    text=True,
    check=False,
)
if result.returncode:
    raise SystemExit("无法加载 Compose 环境配置，请检查本地 .env（未输出配置内容）")

environment = os.environ.copy()
for key in list(environment):
    if key.startswith("QUERY_POSTGRES_"):
        del environment[key]

for line in result.stdout.splitlines():
    key, separator, value = line.partition("=")
    if separator and (key.startswith("QUERY_POSTGRES_") or key == "APP_ENV"):
        environment[key] = value

result = subprocess.run(
    ["go", "-C", "apps/api", "run", "./cmd/query-inspect"],
    cwd=root,
    env=environment,
    check=False,
)
raise SystemExit(result.returncode)
