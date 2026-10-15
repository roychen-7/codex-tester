import { useCallback, useEffect, useRef, useState } from "react";
import {
  Alert,
  Avatar,
  Button,
  Empty,
  Input,
  Modal,
  Space,
  Spin,
  Tag,
  Typography,
} from "antd";
import { api, APIError, errorText } from "./api";
import type { Conversation, Message, User } from "./api";
import AgentPanel from "./AgentPanel";
import MessageEvidence from "./MessageEvidence";
import MessageMarkdown from "./MessageMarkdown";
import styles from "./App.module.css";

const { Text, Title } = Typography;

function remember(key: string, value: string): void {
  try {
    localStorage.setItem(key, value);
  } catch {
    /* Storage may be disabled. */
  }
}

function recalled(key: string): string | null {
  try {
    const value = localStorage.getItem(key);
    return value && /^[1-9]\d*$/.test(value) ? value : null;
  } catch {
    return null;
  }
}

function mergeMessages(current: Message[], incoming: Message[]): Message[] {
  return [
    ...new Map(
      [...current, ...incoming].map((item) => [item.id, item]),
    ).values(),
  ].sort((a, b) =>
    BigInt(a.id) < BigInt(b.id) ? -1 : BigInt(a.id) > BigInt(b.id) ? 1 : 0,
  );
}

interface PendingMessage {
  requestID: string;
  content: string;
  sending: boolean;
  error: string;
}

export default function Workspace({
  user,
  onLogout,
}: {
  user: User;
  onLogout: () => void;
}) {
  const selectionKey = `agent.conversation.${user.id}`;
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [listCursor, setListCursor] = useState("");
  const [listBusy, setListBusy] = useState(false);
  const [selected, setSelected] = useState<string | null>(() =>
    recalled(selectionKey),
  );
  const [active, setActive] = useState<Conversation | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [olderCursor, setOlderCursor] = useState("");
  const [historyBusy, setHistoryBusy] = useState(false);
  const [historyError, setHistoryError] = useState("");
  const [historyReload, setHistoryReload] = useState(0);
  const [olderBusy, setOlderBusy] = useState(false);
  const [pending, setPending] = useState<Record<string, PendingMessage[]>>({});
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [agentBusy, setAgentBusy] = useState(false);
  const [agentRefresh, setAgentRefresh] = useState(0);
  const [error, setError] = useState("");
  const [creating, setCreating] = useState(false);
  const [loggingOut, setLoggingOut] = useState(false);
  const [rename, setRename] = useState<{ id: string; title: string } | null>(
    null,
  );
  const [renaming, setRenaming] = useState(false);
  const selectedRef = useRef(selected);
  const listVersion = useRef(0);
  const mounted = useRef(true);
  const end = useRef<HTMLDivElement>(null);
  selectedRef.current = selected;

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      listVersion.current++;
    };
  }, []);

  const handleError = useCallback(
    (reason: unknown) => {
      if (!mounted.current) return;
      if (reason instanceof APIError && reason.status === 401) onLogout();
      else setError(errorText(reason));
    },
    [onLogout],
  );

  const loadConversations = useCallback(
    async (cursor = "") => {
      const version = ++listVersion.current;
      setListBusy(true);
      try {
        const page = await api.conversations(cursor);
        if (!mounted.current || version !== listVersion.current) return;
        setConversations((current) =>
          cursor
            ? [
                ...new Map(
                  [...current, ...page.items].map((item) => [item.id, item]),
                ).values(),
              ]
            : page.items,
        );
        setListCursor(page.next_cursor);
      } catch (reason) {
        if (version === listVersion.current) handleError(reason);
      } finally {
        if (mounted.current && version === listVersion.current)
          setListBusy(false);
      }
    },
    [handleError],
  );

  useEffect(() => {
    void loadConversations();
  }, [loadConversations]);

  useEffect(() => {
    const controller = new AbortController();
    setAgentBusy(true);
    setActive(null);
    setMessages([]);
    setOlderCursor("");
    setHistoryError("");
    setOlderBusy(false);
    setHistoryBusy(Boolean(selected));
    if (!selected) return () => controller.abort();
    remember(selectionKey, selected);
    Promise.all([
      api.conversation(selected, controller.signal),
      api.messages(selected, "", controller.signal),
    ])
      .then(([conversation, page]) => {
        if (controller.signal.aborted) return;
        setActive(conversation);
        setMessages((current) =>
          mergeMessages(
            current.filter((message) => message.conversation_id === selected),
            page.items,
          ),
        );
        setOlderCursor(page.next_cursor);
      })
      .catch((reason) => {
        if (controller.signal.aborted) return;
        if (reason instanceof APIError && reason.status === 401)
          handleError(reason);
        else setHistoryError(errorText(reason));
      })
      .finally(() => {
        if (!controller.signal.aborted) setHistoryBusy(false);
      });
    return () => controller.abort();
  }, [selected, selectionKey, historyReload, handleError]);

  const updateAgentBusy = useCallback((id: string, busy: boolean) => {
    if (selectedRef.current === id) setAgentBusy(busy);
  }, []);

  const receiveAgentMessages = useCallback(
    (id: string, incoming: Message[]) => {
      if (selectedRef.current !== id) return;
      setMessages((current) => mergeMessages(current, incoming));
      void loadConversations();
    },
    [loadConversations],
  );

  async function createConversation() {
    setCreating(true);
    setError("");
    try {
      const conversation = await api.createConversation();
      if (!mounted.current) return;
      setSelected(conversation.id);
      await loadConversations();
    } catch (reason) {
      handleError(reason);
    } finally {
      setCreating(false);
    }
  }

  async function loadOlder() {
    if (!selected || !olderCursor) return;
    const id = selected;
    setOlderBusy(true);
    try {
      const page = await api.messages(id, olderCursor);
      if (!mounted.current || selectedRef.current !== id) return;
      setMessages((current) => mergeMessages(current, page.items));
      setOlderCursor(page.next_cursor);
    } catch (reason) {
      handleError(reason);
    } finally {
      if (selectedRef.current === id) setOlderBusy(false);
    }
  }

  async function send(id: string, item: PendingMessage) {
    setPending((current) => ({
      ...current,
      [id]: [
        ...(current[id] ?? []).filter(
          (message) => message.requestID !== item.requestID,
        ),
        { ...item, sending: true, error: "" },
      ],
    }));
    try {
      const saved = await api.send(id, item.requestID, item.content);
      if (!mounted.current) return;
      setPending((current) => ({
        ...current,
        [id]: (current[id] ?? []).filter(
          (message) => message.requestID !== item.requestID,
        ),
      }));
      if (selectedRef.current === id) {
        setMessages((current) => mergeMessages(current, [saved]));
        setAgentRefresh((value) => value + 1);
        requestAnimationFrame(() =>
          end.current?.scrollIntoView({ behavior: "smooth" }),
        );
      }
      await loadConversations();
    } catch (reason) {
      if (!mounted.current) return;
      if (reason instanceof APIError && reason.status === 401) {
        onLogout();
        return;
      }
      setPending((current) => ({
        ...current,
        [id]: (current[id] ?? []).map((message) =>
          message.requestID === item.requestID
            ? { ...message, sending: false, error: errorText(reason) }
            : message,
        ),
      }));
    }
  }

  function submit() {
    if (
      !selected ||
      !active ||
      historyBusy ||
      agentBusy ||
      (pending[selected] ?? []).some((item) => item.sending)
    )
      return;
    const content = drafts[selected] ?? "";
    if (!content.trim() || [...content].length > 10000) return;
    setDrafts((current) => ({ ...current, [selected]: "" }));
    void send(selected, {
      requestID: crypto.randomUUID(),
      content,
      sending: true,
      error: "",
    });
  }

  async function saveTitle() {
    if (!rename) return;
    setRenaming(true);
    try {
      const conversation = await api.rename(rename.id, rename.title);
      if (selectedRef.current === conversation.id) setActive(conversation);
      setRename(null);
      await loadConversations();
    } catch (reason) {
      handleError(reason);
    } finally {
      setRenaming(false);
    }
  }

  async function logout() {
    setLoggingOut(true);
    try {
      await api.logout();
      onLogout();
    } catch (reason) {
      handleError(reason);
    } finally {
      setLoggingOut(false);
    }
  }

  const draft = selected ? (drafts[selected] ?? "") : "";
  const pendingHere = selected ? (pending[selected] ?? []) : [];
  const sending = pendingHere.some((item) => item.sending);

  return (
    <div className={styles.workspace}>
      <aside className={styles.sidebar}>
        <div className={styles.brand}>
          <span className={styles.brandMark}>D</span>
          <div>
            <strong>DDSHUB</strong>
            <div className={styles.muted}>运营工作台</div>
          </div>
        </div>
        <Button
          type="primary"
          block
          size="large"
          onClick={() => void createConversation()}
          loading={creating}
        >
          ＋ 新建会话
        </Button>
        <div className={styles.listHeading}>
          <Text type="secondary">历史会话</Text>
          <Button
            size="small"
            type="text"
            loading={listBusy}
            onClick={() => void loadConversations()}
          >
            刷新
          </Button>
        </div>
        <nav className={styles.conversationList} aria-label="历史会话">
          {!listBusy && conversations.length === 0 && (
            <p className={styles.muted}>从一个新问题开始。</p>
          )}
          {conversations.map((item) => (
            <button
              key={item.id}
              className={`${styles.conversation} ${selected === item.id ? styles.selected : ""}`}
              onClick={() => setSelected(item.id)}
              aria-current={selected === item.id ? "page" : undefined}
            >
              <span>{item.title}</span>
              <small>
                {new Date(item.updated_at).toLocaleDateString("zh-CN")}
              </small>
            </button>
          ))}
          {listCursor && (
            <Button
              block
              type="text"
              loading={listBusy}
              onClick={() => void loadConversations(listCursor)}
            >
              加载更多会话
            </Button>
          )}
          {listBusy && conversations.length === 0 && <Spin />}
        </nav>
        <div className={styles.profile}>
          <Avatar>{user.display_name.slice(0, 1)}</Avatar>
          <span>{user.display_name}</span>
          <Button
            type="text"
            onClick={() => void logout()}
            loading={loggingOut}
          >
            退出
          </Button>
        </div>
      </aside>
      <main className={styles.main}>
        <header className={styles.header}>
          <div>
            <Text type="secondary">会话空间</Text>
            <h1>{active?.title ?? "你的运营思考，从这里开始"}</h1>
          </div>
          <Space>
            <Tag color="blue">Agent 对话</Tag>
            {active && (
              <Button
                onClick={() =>
                  setRename({ id: active.id, title: active.title })
                }
              >
                修改标题
              </Button>
            )}
          </Space>
        </header>
        {error && (
          <Alert
            className={styles.notice}
            title={error}
            type="error"
            showIcon
            closable
            onClose={() => setError("")}
          />
        )}
        <div className={styles.history} aria-busy={historyBusy}>
          {historyBusy ? (
            <div className={styles.center}>
              <Spin description="正在加载会话">
                <div className={styles.loadingSpace} />
              </Spin>
            </div>
          ) : historyError ? (
            <div className={styles.center}>
              <Alert title={historyError} type="error" />
              <Button onClick={() => setHistoryReload((value) => value + 1)}>
                重新加载
              </Button>
            </div>
          ) : !selected ? (
            <div className={styles.center}>
              <Empty description="新建一个会话，记录你想了解的问题" />
              <Button
                type="primary"
                onClick={() => void createConversation()}
                loading={creating}
              >
                开始新会话
              </Button>
            </div>
          ) : (
            <div className={styles.messageColumn}>
              {olderCursor && (
                <Button
                  className={styles.older}
                  loading={olderBusy}
                  onClick={() => void loadOlder()}
                >
                  加载更早消息
                </Button>
              )}
              {messages.length === 0 && pendingHere.length === 0 && (
                <div className={styles.emptyConversation}>
                  <Title level={3}>今天想了解什么？</Title>
                  <Text type="secondary">
                    先记录问题，让每一次思考都有迹可循。
                  </Text>
                </div>
              )}
              {messages.map((item) => (
                <article
                  key={item.id}
                  data-role={item.role}
                  className={`${styles.message} ${item.role === "user" ? styles.userMessage : styles.agentMessage}`}
                >
                  <div className={styles.messageMeta}>
                    <strong>
                      {item.role === "user" ? user.display_name : "Agent"}
                    </strong>
                    <time>
                      {new Date(item.created_at).toLocaleString("zh-CN")}
                    </time>
                    <span>已保存</span>
                  </div>
                  <div className={styles.bubble}>
                    {item.role === "assistant" ? (
                      <MessageMarkdown content={item.content} />
                    ) : (
                      item.content
                    )}
                  </div>
                  {item.has_evidence && (
                    <div className={styles.messageEvidence}>
                      <MessageEvidence
                        conversation={selected}
                        message={item.id}
                        onOpenConversation={setSelected}
                      />
                    </div>
                  )}
                </article>
              ))}
              {pendingHere
                .filter(
                  (item) =>
                    !messages.some(
                      (message) => message.request_id === item.requestID,
                    ),
                )
                .map((item) => (
                  <article
                    key={item.requestID}
                    data-role="user"
                    className={`${styles.message} ${styles.userMessage}`}
                  >
                    <div className={styles.messageMeta}>
                      <strong>{user.display_name}</strong>
                      <span>{item.sending ? "正在保存…" : "未确认保存"}</span>
                    </div>
                    <div className={`${styles.bubble} ${styles.pendingBubble}`}>
                      {item.content}
                    </div>
                    {item.error && (
                      <div className={styles.retry}>
                        <Text type="danger">{item.error}</Text>
                        <Button
                          size="small"
                          onClick={() => selected && void send(selected, item)}
                        >
                          重试保存
                        </Button>
                      </div>
                    )}
                  </article>
                ))}
              <AgentPanel
                key={selected}
                conversation={selected}
                refresh={agentRefresh}
                onBusy={updateAgentBusy}
                onMessages={receiveAgentMessages}
                onError={handleError}
              />
              <div ref={end} />
            </div>
          )}
        </div>
        {selected && (
          <footer className={styles.composer}>
            <Input.TextArea
              aria-label="消息内容"
              placeholder="输入你的问题…"
              autoSize={{ minRows: 2, maxRows: 6 }}
              value={draft}
              disabled={!active || historyBusy}
              onChange={(event) =>
                setDrafts((current) => ({
                  ...current,
                  [selected]: event.target.value,
                }))
              }
              onKeyDown={(event) => {
                if (
                  (event.ctrlKey || event.metaKey) &&
                  event.key === "Enter" &&
                  !event.nativeEvent.isComposing &&
                  !sending
                ) {
                  event.preventDefault();
                  submit();
                }
              }}
            />
            <div className={styles.composerBar}>
              <Text type="secondary">
                {[...draft].length.toLocaleString()} / 10,000 · ⌘ / Ctrl + Enter
                发送
              </Text>
              <Button
                type="primary"
                loading={sending}
                disabled={
                  !active ||
                  historyBusy ||
                  agentBusy ||
                  sending ||
                  !draft.trim() ||
                  [...draft].length > 10000
                }
                onClick={submit}
              >
                发送消息
              </Button>
            </div>
            <p className={styles.stageNote}>
              可以直接提问或补充上下文，Agent 会在需要时向你追问。
            </p>
          </footer>
        )}
      </main>
      <Modal
        title="修改会话标题"
        open={rename !== null}
        onCancel={() => setRename(null)}
        onOk={() => void saveTitle()}
        confirmLoading={renaming}
        okButtonProps={{
          disabled:
            !rename?.title.trim() || [...(rename?.title ?? "")].length > 200,
        }}
        okText="保存"
        cancelText="取消"
      >
        <Input
          aria-label="会话标题"
          value={rename?.title ?? ""}
          onChange={(event) =>
            setRename((current) =>
              current ? { ...current, title: event.target.value } : null,
            )
          }
          maxLength={200}
        />
      </Modal>
    </div>
  );
}
