package repository

import (
	"gorm.io/gorm"
	"voex-server/internal/shared"
)

func ActiveSharedDocument(db *gorm.DB, tokenHash string) shared.Row {
	return One(DocumentQuery(db, true).Select(DocumentColumns+",d.file_content,s.permission").Joins("JOIN file_shares s ON s.document_id=d.id").Where("s.token_hash=? AND s.revoked_at IS NULL", tokenHash))
}

func ListDocumentShares(db *gorm.DB, documentID any) []shared.Row {
	return Rows(db.Table("file_shares AS s").Select("s.id,s.permission,s.created_at,s.revoked_at,u.id AS creator_id,u.name,u.avator").Joins("JOIN users u ON u.id=s.created_by").Where("s.document_id=?", documentID).Order("s.created_at DESC,s.id"))
}

func CreateDocumentShare(db *gorm.DB, share shared.Row) {
	Must(db.Table("file_shares").Create(map[string]any(share)))
}

func DocumentShareByID(db *gorm.DB, documentID any, id string) shared.Row {
	return One(db.Table("file_shares").Select("created_by").Where("id=? AND document_id=?", id, documentID))
}

func RevokeDocumentShare(db *gorm.DB, id string) {
	Must(db.Table("file_shares").Where("id=?", id).Update("revoked_at", gorm.Expr("COALESCE(revoked_at,CURRENT_TIMESTAMP())")))
}

func ListSharedAttachments(db *gorm.DB, fileKey string) []shared.Row {
	return Rows(db.Table("files AS f").Select("f.hash,f.name,f.mime,u.id AS creator_id,u.name AS creator_name").Joins("LEFT JOIN users u ON u.id=f.creator_id").Where("f.file_key=?", fileKey).Order("f.id"))
}

func UpdateSharedContent(db *gorm.DB, documentID any, content string) {
	Must(db.Table("documents").Where("id=?", documentID).Updates(map[string]any{"file_content": content, "revision": gorm.Expr("revision+1")}))
}

func SharedCreatorAvatar(db *gorm.DB, assetID string, creatorID any) shared.Row {
	return One(db.Table("assets").Where("id=? AND creator_id=? AND directory='avator' AND mime='image/webp'", assetID, creatorID))
}
