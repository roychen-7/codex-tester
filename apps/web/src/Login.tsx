import { useState } from "react";
import { Alert, Button, Card, Form, Input, Typography } from "antd";
import { api, errorText } from "./api";
import type { User } from "./api";
import styles from "./App.module.css";

const { Text, Title } = Typography;

export default function Login({ onLogin }: { onLogin: (user: User) => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function submit(values: { username: string; password: string }) {
    setBusy(true);
    setError("");
    try {
      onLogin(await api.login(values.username, values.password));
    } catch (reason) {
      setError(errorText(reason));
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className={styles.login}>
      <Card className={styles.loginCard}>
        <div className={styles.brandMark}>D</div>
        <Text type="secondary">DDSHUB · 运营工作台</Text>
        <Title level={2}>继续你的思考</Title>
        <p className={styles.muted}>登录后，保存问题并整理每一次对话。</p>
        {error && (
          <Alert
            title={error}
            type="error"
            showIcon
            className={styles.notice}
          />
        )}
        <Form layout="vertical" onFinish={submit} requiredMark={false}>
          <Form.Item
            label="用户名"
            name="username"
            rules={[{ required: true, message: "请输入用户名" }]}
          >
            <Input
              size="large"
              autoComplete="username"
              maxLength={64}
              autoFocus
            />
          </Form.Item>
          <Form.Item
            label="密码"
            name="password"
            rules={[{ required: true, message: "请输入密码" }]}
          >
            <Input.Password size="large" autoComplete="current-password" />
          </Form.Item>
          <Button
            block
            size="large"
            type="primary"
            htmlType="submit"
            loading={busy}
          >
            登录工作台
          </Button>
        </Form>
        <Text type="secondary">账号由管理员创建。</Text>
      </Card>
    </main>
  );
}
