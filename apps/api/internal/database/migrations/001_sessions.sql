CREATE TABLE IF NOT EXISTS agent_users (
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

CREATE TABLE IF NOT EXISTS auth_sessions (
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

CREATE TABLE IF NOT EXISTS conversations (
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

CREATE TABLE IF NOT EXISTS messages (
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

