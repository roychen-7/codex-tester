ALTER TABLE messages
    ADD UNIQUE KEY uq_messages_id_conversation (id, conversation_id);
