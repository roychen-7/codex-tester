"""Fill missing local configuration without replacing existing values."""
from pathlib import Path
import secrets

root = Path(__file__).resolve().parents[1]
target = root / ".env"
text = target.read_text() if target.exists() else (root / ".env.example").read_text()
lines = text.splitlines()
for key in ("MYSQL_PASSWORD", "MYSQL_ROOT_PASSWORD"):
    matches = [i for i, line in enumerate(lines) if line.startswith(key + "=")]
    if not matches:
        lines.append(f"{key}={secrets.token_hex(24)}")
    elif lines[matches[-1]].split("=", 1)[1].strip() in ("", '""', "''"):
        lines[matches[-1]] = f"{key}={secrets.token_hex(24)}"
target.write_text("\n".join(lines) + "\n")
target.chmod(0o600)
print("本地 .env 已就绪；已有非空密码保持不变。")
