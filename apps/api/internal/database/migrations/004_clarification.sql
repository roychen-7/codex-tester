CREATE TABLE IF NOT EXISTS agent_tasks (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    conversation_id BIGINT UNSIGNED NOT NULL,
    origin_message_id BIGINT UNSIGNED NOT NULL,
    status VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'clarifying',
    requirement JSON NOT NULL COMMENT '动态需求；关键项包含来源消息及用户说明、历史参考、模型建议的区分',
    missing_information JSON NOT NULL COMMENT '待澄清问题数组',
    revision INT UNSIGNED NOT NULL DEFAULT 1,
    confirmed_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    active_conversation_id BIGINT UNSIGNED GENERATED ALWAYS AS (
        CASE WHEN status IN ('clarifying', 'awaiting_confirmation') THEN conversation_id ELSE NULL END
    ) STORED,
    PRIMARY KEY (id),
    UNIQUE KEY uq_tasks_origin (origin_message_id),
    UNIQUE KEY uq_tasks_id_conversation (id, conversation_id),
    UNIQUE KEY uq_tasks_active_conversation (active_conversation_id),
    KEY idx_tasks_conversation_recent (conversation_id, updated_at DESC, id DESC),
    CONSTRAINT fk_tasks_conversation FOREIGN KEY (conversation_id)
        REFERENCES conversations (id) ON DELETE RESTRICT,
    CONSTRAINT fk_tasks_origin FOREIGN KEY (origin_message_id, conversation_id)
        REFERENCES messages (id, conversation_id) ON DELETE RESTRICT,
    CONSTRAINT chk_tasks_status CHECK (status IN ('clarifying', 'awaiting_confirmation', 'confirmed', 'cancelled')),
    CONSTRAINT chk_tasks_revision CHECK (revision > 0),
    CONSTRAINT chk_tasks_requirement CHECK (JSON_TYPE(requirement) = 'OBJECT'),
    CONSTRAINT chk_tasks_missing CHECK (JSON_TYPE(missing_information) = 'ARRAY'),
    CONSTRAINT chk_tasks_confirmation CHECK (
        (status = 'confirmed' AND confirmed_at IS NOT NULL)
        OR (status <> 'confirmed' AND confirmed_at IS NULL)
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
    COMMENT='需求澄清任务；已确认只表示需求确认，不代表分析完成';

CREATE TABLE IF NOT EXISTS llm_runs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    conversation_id BIGINT UNSIGNED NOT NULL,
    task_id BIGINT UNSIGNED NULL,
    trigger_message_id BIGINT UNSIGNED NOT NULL,
    attempt_no INT UNSIGNED NOT NULL,
    status VARCHAR(24) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'pending',
    model VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    prompt_version VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    provider_response_id VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NULL,
    context_snapshot JSON NOT NULL COMMENT '消息引用、摘要、历史来源和需求版本；不存凭据',
    result JSON NULL COMMENT '后端校验后的公开回复与需求更新，不存模型内部推理',
    assistant_message_id BIGINT UNSIGNED NULL,
    input_tokens BIGINT UNSIGNED NULL COMMENT '未知时为 NULL，不假定为 0',
    output_tokens BIGINT UNSIGNED NULL,
    error_code VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    error_message VARCHAR(500) NULL COMMENT '脱敏错误，不存上游原始错误正文',
    started_at DATETIME(6) NULL,
    finished_at DATETIME(6) NULL,
    deadline_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    active_conversation_id BIGINT UNSIGNED GENERATED ALWAYS AS (
        CASE WHEN status IN ('pending', 'running') THEN conversation_id ELSE NULL END
    ) STORED,
    successful_trigger_id BIGINT UNSIGNED GENERATED ALWAYS AS (
        CASE WHEN status = 'succeeded' THEN trigger_message_id ELSE NULL END
    ) STORED,
    PRIMARY KEY (id),
    UNIQUE KEY uq_runs_attempt (trigger_message_id, attempt_no),
    UNIQUE KEY uq_runs_active_conversation (active_conversation_id),
    UNIQUE KEY uq_runs_successful_trigger (successful_trigger_id),
    UNIQUE KEY uq_runs_assistant_message (assistant_message_id),
    KEY idx_runs_conversation_recent (conversation_id, id DESC),
    KEY idx_runs_status_deadline (status, deadline_at),
    CONSTRAINT fk_runs_conversation FOREIGN KEY (conversation_id)
        REFERENCES conversations (id) ON DELETE RESTRICT,
    CONSTRAINT fk_runs_task FOREIGN KEY (task_id, conversation_id)
        REFERENCES agent_tasks (id, conversation_id) ON DELETE RESTRICT,
    CONSTRAINT fk_runs_trigger FOREIGN KEY (trigger_message_id, conversation_id)
        REFERENCES messages (id, conversation_id) ON DELETE RESTRICT,
    CONSTRAINT fk_runs_assistant FOREIGN KEY (assistant_message_id, conversation_id)
        REFERENCES messages (id, conversation_id) ON DELETE RESTRICT,
    CONSTRAINT chk_runs_status CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'cancelled')),
    CONSTRAINT chk_runs_attempt CHECK (attempt_no > 0),
    CONSTRAINT chk_runs_model CHECK (CHAR_LENGTH(TRIM(model)) > 0 AND CHAR_LENGTH(TRIM(prompt_version)) > 0),
    CONSTRAINT chk_runs_context CHECK (JSON_TYPE(context_snapshot) = 'OBJECT'),
    CONSTRAINT chk_runs_result CHECK (result IS NULL OR JSON_TYPE(result) = 'OBJECT'),
    CONSTRAINT chk_runs_finish CHECK (
        (status IN ('pending', 'running') AND finished_at IS NULL)
        OR (status IN ('succeeded', 'failed', 'cancelled') AND finished_at IS NOT NULL)
    ),
    CONSTRAINT chk_runs_running CHECK (
        status <> 'running' OR (started_at IS NOT NULL AND deadline_at IS NOT NULL AND deadline_at > started_at)
    ),
    CONSTRAINT chk_runs_success CHECK (
        (status = 'succeeded' AND assistant_message_id IS NOT NULL AND result IS NOT NULL AND started_at IS NOT NULL)
        OR (status <> 'succeeded' AND assistant_message_id IS NULL AND result IS NULL)
    ),
    CONSTRAINT chk_runs_duration CHECK (finished_at IS NULL OR started_at IS NULL OR finished_at >= started_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
    COMMENT='每次模型调用尝试独立记录；状态条件更新阻止超时旧结果落库';
