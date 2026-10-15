ALTER TABLE conversations
    ADD COLUMN context_summary MEDIUMTEXT NULL COMMENT '上下文摘要；不能替代原始消息或用户确认',
    ADD COLUMN summary_through_message_id BIGINT UNSIGNED NULL COMMENT '摘要覆盖到的最后一条消息',
    ADD CONSTRAINT fk_conversations_summary_message
        FOREIGN KEY (summary_through_message_id, id)
        REFERENCES messages (id, conversation_id) ON DELETE RESTRICT,
    ADD CONSTRAINT chk_conversations_summary_pair CHECK (
        (context_summary IS NULL AND summary_through_message_id IS NULL)
        OR (context_summary IS NOT NULL AND CHAR_LENGTH(TRIM(context_summary)) > 0
            AND summary_through_message_id IS NOT NULL)
    );
