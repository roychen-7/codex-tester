package database

import (
	"errors"

	"gorm.io/gorm"
)

// recoveredAlter handles a crash between an atomic ALTER and its ledger write.
// The SQL files are immutable. A partially present marker set signals drift,
// not a resumable migration; never suppress arbitrary duplicate-column errors.
func recoveredAlter(conn *gorm.DB, name string) (bool, error) {
	switch name {
	case "002_message_scope.sql":
		var count int64
		err := conn.Table("information_schema.STATISTICS").
			Where("TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = ?", "messages", "uq_messages_id_conversation").
			Where("NON_UNIQUE = 0 AND ((SEQ_IN_INDEX = 1 AND COLUMN_NAME = ?) OR (SEQ_IN_INDEX = 2 AND COLUMN_NAME = ?))", "id", "conversation_id").
			Count(&count).Error
		if err != nil {
			return false, err
		}

		return completeMarkers(count, 2)
	case "003_conversation_context.sql":
		var columns, constraints int64
		err := conn.Table("information_schema.COLUMNS").
			Where("TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME IN ?", "conversations", []string{"context_summary", "summary_through_message_id"}).
			Count(&columns).Error
		if err != nil {
			return false, err
		}

		err = conn.Table("information_schema.TABLE_CONSTRAINTS").
			Where("CONSTRAINT_SCHEMA = DATABASE() AND TABLE_NAME = ? AND CONSTRAINT_NAME IN ?", "conversations", []string{"fk_conversations_summary_message", "chk_conversations_summary_pair"}).
			Count(&constraints).Error
		if err != nil {
			return false, err
		}

		return completeMarkers(columns+constraints, 4)
	default:
		return false, nil
	}
}

func completeMarkers(count, expected int64) (bool, error) {
	if count == 0 {
		return false, nil
	}

	if count != expected {
		return false, errors.New("迁移目标结构不完整，请检查数据库结构后重试")
	}

	return true, nil
}
