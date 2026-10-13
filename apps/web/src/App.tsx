import { useCallback, useEffect, useState } from "react";
import { Alert, Button, ConfigProvider, Spin } from "antd";
import zhCN from "antd/locale/zh_CN";
import { api, APIError, errorText } from "./api";
import type { User } from "./api";
import Login from "./Login";
import Workspace from "./Workspace";
import styles from "./App.module.css";

export default function App() {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const logout = useCallback(() => setUser(null), []);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError("");
    api
      .me(controller.signal)
      .then((value) => {
        if (!controller.signal.aborted) setUser(value);
      })
      .catch((reason) => {
        if (controller.signal.aborted) return;
        if (reason instanceof APIError && reason.status === 401) setUser(null);
        else setError(errorText(reason));
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [attempt]);

  return (
    <ConfigProvider
      locale={zhCN}
      button={{ autoInsertSpace: false }}
      theme={{
        token: {
          colorPrimary: "#245e57",
          borderRadius: 10,
          fontFamily:
            'Inter, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
        },
      }}
    >
      {loading ? (
        <div className={styles.boot}>
          <Spin size="large" />
        </div>
      ) : error ? (
        <div className={styles.boot}>
          <Alert title={error} type="error" />
          <Button onClick={() => setAttempt((value) => value + 1)}>
            重试连接
          </Button>
        </div>
      ) : user ? (
        <Workspace key={user.id} user={user} onLogout={logout} />
      ) : (
        <Login onLogin={setUser} />
      )}
    </ConfigProvider>
  );
}
