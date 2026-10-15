CREATE TABLE IF NOT EXISTS agent_runs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    conversation_id BIGINT UNSIGNED NOT NULL,
    trigger_message_id BIGINT UNSIGNED NOT NULL,
    attempt_no INT UNSIGNED NOT NULL,
    status VARCHAR(24) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    model VARCHAR(128) NOT NULL,
    prompt_version VARCHAR(64) NOT NULL,
    context_snapshot JSON NOT NULL,
    result JSON NULL,
    assistant_message_id BIGINT UNSIGNED NULL,
    error_message VARCHAR(1000) NULL,
    started_at DATETIME(6) NULL,
    finished_at DATETIME(6) NULL,
    deadline_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    active_conversation_id BIGINT UNSIGNED GENERATED ALWAYS AS (
        CASE WHEN status IN ('pending','running') THEN conversation_id ELSE NULL END
    ) STORED,
    answered_message_id BIGINT UNSIGNED GENERATED ALWAYS AS (
        CASE WHEN status IN ('succeeded','waiting_for_user') THEN trigger_message_id ELSE NULL END
    ) STORED,
    PRIMARY KEY (id),
    UNIQUE KEY uq_agent_attempt (trigger_message_id,attempt_no),
    UNIQUE KEY uq_agent_active (active_conversation_id),
    UNIQUE KEY uq_agent_answered (answered_message_id),
    UNIQUE KEY uq_agent_reply (assistant_message_id),
    KEY idx_agent_recent (conversation_id,id DESC),
    KEY idx_agent_queue (status,deadline_at),
    CONSTRAINT fk_agent_run_conversation FOREIGN KEY (conversation_id) REFERENCES conversations(id),
    CONSTRAINT fk_agent_run_trigger FOREIGN KEY (trigger_message_id,conversation_id) REFERENCES messages(id,conversation_id),
    CONSTRAINT fk_agent_run_reply FOREIGN KEY (assistant_message_id,conversation_id) REFERENCES messages(id,conversation_id),
    CONSTRAINT chk_agent_run_status CHECK (status IN ('pending','running','waiting_for_user','succeeded','failed','cancelled')),
    CONSTRAINT chk_agent_run_attempt CHECK (attempt_no>0),
    CONSTRAINT chk_agent_run_context CHECK (JSON_TYPE(context_snapshot)='OBJECT'),
    CONSTRAINT chk_agent_run_result CHECK (result IS NULL OR JSON_TYPE(result)='OBJECT'),
    CONSTRAINT chk_agent_run_timing CHECK (
        (status IN ('pending','running') AND deadline_at IS NOT NULL AND finished_at IS NULL)
        OR (status IN ('waiting_for_user','succeeded','failed','cancelled') AND deadline_at IS NULL AND finished_at IS NOT NULL)
    ),
    CONSTRAINT chk_agent_run_answer CHECK (
        (status IN ('waiting_for_user','succeeded') AND result IS NOT NULL AND assistant_message_id IS NOT NULL)
        OR (status IN ('pending','running','failed','cancelled') AND result IS NULL AND assistant_message_id IS NULL)
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
CREATE TABLE IF NOT EXISTS agent_run_steps (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    agent_run_id BIGINT UNSIGNED NOT NULL,
    sequence_no INT UNSIGNED NOT NULL,
    kind VARCHAR(20) NOT NULL,
    name VARCHAR(128) NOT NULL,
    status VARCHAR(20) NOT NULL,
    output JSON NULL,
    provider_response_id VARCHAR(255) NULL,
    input_tokens BIGINT UNSIGNED NULL,
    output_tokens BIGINT UNSIGNED NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_agent_run_step (agent_run_id,sequence_no),
    CONSTRAINT fk_agent_step_run FOREIGN KEY (agent_run_id) REFERENCES agent_runs(id),
    CONSTRAINT chk_agent_step_status CHECK (status IN ('running','succeeded','failed','cancelled')),
    CONSTRAINT chk_agent_step_kind CHECK (kind IN ('model','tool'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
