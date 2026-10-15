package repository

import (
	"voex-server/internal/shared"

	"gorm.io/gorm"
)

// Lists omit the potentially large immutable snapshot; it is fetched by review ID on demand.
func aiTurnQuery(db *gorm.DB, user string, snapshot bool) *gorm.DB {
	fields := "t.id, t.user_id, t.document_id, t.turn_id, t.model_id, t.prompt, t.response, t.activity, t.error, t.review_id, t.finish_hash, t.created_at, t.completed_at, d.file_key AS source_key, JSON_LENGTH(t.review_json, '$.changes') AS file_count, a.review_id AS applied_review_id"
	if snapshot {
		fields += ", t.review_json"
	}
	return db.Table("ai_conversation_turns AS t").Select(fields).
		Joins("JOIN documents AS d ON d.id=t.document_id").
		Joins("LEFT JOIN ai_review_applications AS a ON a.user_id=t.user_id AND a.review_id=t.review_id").
		Where("t.user_id=?", user)
}

func AIConversation(db *gorm.DB, user string, documentID any, before uint64, limit int) []shared.Row {
	query := aiTurnQuery(db, user, false).Where("t.document_id=?", documentID)
	if before > 0 {
		query = query.Where("t.id < ?", before)
	}
	return Rows(query.Order("t.id DESC").Limit(limit))
}

func AITurn(db *gorm.DB, user, id string) shared.Row {
	return One(aiTurnQuery(db, user, false).Where("t.turn_id=?", id))
}

func AIStoredReview(db *gorm.DB, user, id string) shared.Row {
	return One(aiTurnQuery(db, user, true).Where("t.review_id=? AND t.completed_at IS NOT NULL", id))
}

func InsertAITurn(db *gorm.DB, user, id string, documentID any, model, prompt string) {
	Must(db.Table("ai_conversation_turns").Create(map[string]any{
		"user_id": user, "turn_id": id, "document_id": documentID, "model_id": model,
		"prompt": prompt, "response": "", "activity": "[]", "error": "",
	}))
}

func FinishAITurn(db *gorm.DB, user, id string, values shared.Row) {
	values["completed_at"] = gorm.Expr("CURRENT_TIMESTAMP(3)")
	Must(db.Table("ai_conversation_turns").Where("user_id=? AND turn_id=? AND completed_at IS NULL", user, id).Updates(map[string]any(values)))
}
