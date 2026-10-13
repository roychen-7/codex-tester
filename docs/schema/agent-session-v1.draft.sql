-- 已确认的设计说明副本：Agent 第 1 步，会话与登录基础。
-- 实际执行文件为 apps/api/internal/database/migrations/001_sessions.sql；勿直接运行此设计副本。
-- 目标：MySQL 8.0.44（CHECK 约束要求至少 8.0.16） / InnoDB / utf8mb4。仅用于 Agent 独立数据库。
-- ddshub PostgreSQL 不执行本文件；其用户、业务表不复制到此数据库。
-- 所有时间按 UTC 写入和读取；连接 time_zone 必须设为 +00:00。
-- BIGINT 标识符在 HTTP JSON 中返回字符串，避免浏览器数值精度损失。
-- 范围：初始化账号、登录/退出、新建/重命名/切换会话、发送文字、历史分页、用户隔离。
-- 首批账号由服务端初始化命令创建；不存储公开注册、找回密码、文件、分享或删除相关状态。
-- 本阶段不生成 AI 回复；assistant 仅保留角色取值，工具消息与分析任务后续独立迁移。
-- 建议初始参数：登录有效期 7 天（固定期限、不自动续期）；列表每页 20 条、最多 100 条。
-- 参数由后端配置与接口校验实现，expires_at 在创建登录会话时明确写入。

CREATE TABLE agent_users (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    username VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL
        COMMENT '登录名：服务端校验为小写字母、数字、点、下划线或连字符',
    display_name VARCHAR(100) NOT NULL,
    password_hash VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL
        COMMENT '含算法参数与盐的密码哈希，不保存明文密码',
    status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'active',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_agent_users_username (username),
    CONSTRAINT chk_agent_users_status CHECK (status IN ('active', 'disabled'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
    COMMENT='Agent 登录用户，独立于 ddshub 客户';

CREATE TABLE auth_sessions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id BIGINT UNSIGNED NOT NULL,
    token_hash BINARY(32) NOT NULL COMMENT '随机会话令牌的 SHA-256；原令牌只在 HttpOnly Cookie 中传递',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    expires_at DATETIME(6) NOT NULL,
    revoked_at DATETIME(6) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_auth_sessions_token_hash (token_hash),
    KEY idx_auth_sessions_user (user_id),
    KEY idx_auth_sessions_expiry (expires_at),
    CONSTRAINT fk_auth_sessions_user FOREIGN KEY (user_id) REFERENCES agent_users (id) ON DELETE RESTRICT,
    CONSTRAINT chk_auth_sessions_expiry CHECK (expires_at > created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
    COMMENT='浏览器登录会话；每次请求检查过期、撤销及用户禁用状态';

CREATE TABLE conversations (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id BIGINT UNSIGNED NOT NULL,
    title VARCHAR(200) NOT NULL DEFAULT '新会话',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    last_message_at DATETIME(6) NULL,
    PRIMARY KEY (id),
    KEY idx_conversations_user_recent (user_id, updated_at DESC, id DESC),
    CONSTRAINT fk_conversations_user FOREIGN KEY (user_id) REFERENCES agent_users (id) ON DELETE RESTRICT,
    CONSTRAINT chk_conversations_title CHECK (CHAR_LENGTH(TRIM(title)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
    COMMENT='用户拥有的聊天会话；会话列表按 updated_at 与 id 做游标分页';

CREATE TABLE messages (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    conversation_id BIGINT UNSIGNED NOT NULL,
    request_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL
        COMMENT '单条消息幂等 UUID；重试复用，服务端校验格式',
    role VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL
        COMMENT '用户端只能提交 user；assistant 由后端创建',
    content MEDIUMTEXT NOT NULL COMMENT '文本正文；接口限制单条消息大小，不依赖字段容量作为限制',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_messages_request (conversation_id, request_id),
    KEY idx_messages_conversation_order (conversation_id, id),
    CONSTRAINT fk_messages_conversation FOREIGN KEY (conversation_id) REFERENCES conversations (id) ON DELETE RESTRICT,
    CONSTRAINT chk_messages_role CHECK (role IN ('user', 'assistant')),
    CONSTRAINT chk_messages_content CHECK (CHAR_LENGTH(content) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
    COMMENT='会话消息；按 id 游标分页，第一阶段仅写入真实用户消息';

-- 必须由后端落实的事务与权限规则：
-- 1. 所有会话/消息读取、写入，均以登录态 user_id 校验 conversations.user_id。
--    外键仅保证关联存在，不替代用户隔离。跨用户访问统一返回 404。
-- 2. 保存消息时，在事务内锁定该用户拥有的会话，插入消息并更新
--    conversations.last_message_at / updated_at；任何失败整体回滚。
-- 3. 相同会话 + request_id + 相同内容的重试返回原消息；内容不同返回 409。
--    只有首次插入更新会话活动时间，幂等重试不修改历史排序。
-- 4. 禁用用户即时失去访问权；退出登录撤销当前 auth_session；修改密码撤销所有登录会话。
-- 5. 第一步不提供账号/会话删除入口。RESTRICT 防止意外级联删除；后续单独设计删除与保留策略。
-- 6. Cookie 使用 HttpOnly、SameSite，并在 HTTPS 环境设置 Secure；写请求做 CSRF 防护。
-- 7. 后续迁移再添加分析任务、已确认条件、历史摘要、工具执行及结果表；
--    一个 conversation 可以包含多个分析任务，不能把任务条件固定绑定到整段会话。
-- 8. 登录名规范化后按唯一索引查询；用户名格式、标题去空白、正文非全空白等仍由后端校验。
--    单条消息建议上限 10000 个 Unicode 码点，同时限制请求体大小为 64 KiB；保留原正文格式。
-- 9. 登录签发密码学安全的随机令牌；数据库只保存令牌哈希，密码使用专用密码哈希算法。
--    密码哈希/令牌哈希不返回前端、不写应用日志。登录失败限速由接口层实现。
-- 10. messages 只保存已持久化消息；发送中、失败待重试由前端管理，不伪造失败消息记录。
--     网络超时后复用 request_id；正文和角色按应用层精确比较，不使用数据库的宽松文本排序规则比较。
-- 11. 读取消息先验证所属用户，再以 conversation_id + id 游标查询：
--     最近一页 ORDER BY id DESC；更早消息 WHERE id < before_id ORDER BY id DESC，前端按正序展示。
--     会话列表按 (updated_at, id) 降序分页；新消息/改名会使会话前移，前端按 id 去重并刷新第一页。
-- 12. 禁用与改密不是第一阶段的管理页面功能，但鉴权需检查 status；过期/撤销登录记录可定时清理。
