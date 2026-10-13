"""Read a new password without echoing it or putting it on a process command line."""
import getpass
import subprocess
import sys

password = getpass.getpass("新用户密码（12–72 字节）：")
confirmation = getpass.getpass("再次输入密码：")
if password != confirmation:
    raise SystemExit("两次密码不一致，未创建账号。")
result = subprocess.run(
    ["docker", "compose", "exec", "-T", "api", "go", "run", "./cmd/manage",
     "create-user", sys.argv[1], sys.argv[2]],
    input=password + "\n", text=True,
)
raise SystemExit(result.returncode)
