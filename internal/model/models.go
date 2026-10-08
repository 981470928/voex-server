// Package model maps the existing MySQL schema; schema changes use explicit migrations.
package model

import "time"

type Asset struct {
	ID          string    `gorm:"column:id;primaryKey" json:"-"`
	CreatorID   string    `gorm:"column:creator_id" json:"-"`
	Directory   string    `gorm:"column:directory" json:"-"`
	StorageName string    `gorm:"column:storage_name" json:"-"`
	Name        string    `gorm:"column:name" json:"-"`
	MIME        string    `gorm:"column:mime" json:"-"`
	Size        int64     `gorm:"column:size" json:"-"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime:false" json:"-"`
}

func (Asset) TableName() string { return "assets" }

type AuthSession struct {
	ID          string    `gorm:"column:id;primaryKey" json:"-"`
	UserID      string    `gorm:"column:user_id" json:"-"`
	RefreshHash string    `gorm:"column:refresh_hash" json:"-"`
	ExpiresAt   time.Time `gorm:"column:expires_at" json:"-"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime:false" json:"-"`
}

func (AuthSession) TableName() string { return "auth_sessions" }

type Document struct {
	ID          int64      `gorm:"column:id;primaryKey;autoIncrement" json:"-"`
	FileKey     string     `gorm:"column:file_key" json:"-"`
	FileName    string     `gorm:"column:file_name" json:"-"`
	FileContent *string    `gorm:"column:file_content" json:"-"`
	CreatedAt   *time.Time `gorm:"column:created_at;autoCreateTime:false" json:"-"`
	UpdatedAt   *time.Time `gorm:"column:updated_at;autoUpdateTime:false" json:"-"`
	FolderID    int64      `gorm:"column:folder_id" json:"-"`
	CreatorID   *string    `gorm:"column:creator_id" json:"-"`
	Privileges  string     `gorm:"column:privileges" json:"-"`
	Revision    int64      `gorm:"column:revision" json:"-"`
}

func (Document) TableName() string { return "documents" }

type FileShare struct {
	ID         string     `gorm:"column:id;primaryKey" json:"-"`
	DocumentID int64      `gorm:"column:document_id" json:"-"`
	TokenHash  string     `gorm:"column:token_hash" json:"-"`
	Permission string     `gorm:"column:permission" json:"-"`
	CreatedBy  string     `gorm:"column:created_by" json:"-"`
	CreatedAt  time.Time  `gorm:"column:created_at;autoCreateTime:false" json:"-"`
	RevokedAt  *time.Time `gorm:"column:revoked_at" json:"-"`
}

func (FileShare) TableName() string { return "file_shares" }

type File struct {
	ID         int64      `gorm:"column:id;primaryKey;autoIncrement" json:"-"`
	FileKey    string     `gorm:"column:file_key" json:"-"`
	Hash       string     `gorm:"column:hash" json:"-"`
	Name       string     `gorm:"column:name" json:"-"`
	MIME       string     `gorm:"column:mime" json:"-"`
	Size       int64      `gorm:"column:size" json:"-"`
	CreatedAt  *time.Time `gorm:"column:created_at;autoCreateTime:false" json:"-"`
	CreatorID  *string    `gorm:"column:creator_id" json:"-"`
	Privileges string     `gorm:"column:privileges" json:"-"`
}

func (File) TableName() string { return "files" }

type Folder struct {
	ID         int64     `gorm:"column:id;primaryKey;autoIncrement" json:"-"`
	FolderKey  string    `gorm:"column:folder_key" json:"-"`
	ProjectID  int64     `gorm:"column:project_id" json:"-"`
	ParentID   *int64    `gorm:"column:parent_id" json:"-"`
	Name       string    `gorm:"column:name" json:"-"`
	CreatedAt  time.Time `gorm:"column:created_at;autoCreateTime:false" json:"-"`
	UpdatedAt  time.Time `gorm:"column:updated_at;autoUpdateTime:false" json:"-"`
	Privileges string    `gorm:"column:privileges" json:"-"`
}

func (Folder) TableName() string { return "folders" }

type ProjectMember struct {
	ProjectID int64  `gorm:"column:project_id;primaryKey;autoIncrement:false" json:"-"`
	UserID    string `gorm:"column:user_id;primaryKey" json:"-"`
}

func (ProjectMember) TableName() string { return "project_members" }

type Project struct {
	ID         int64     `gorm:"column:id;primaryKey;autoIncrement" json:"-"`
	ProjectKey string    `gorm:"column:project_key" json:"-"`
	Name       string    `gorm:"column:name" json:"-"`
	CreatedAt  time.Time `gorm:"column:created_at;autoCreateTime:false" json:"-"`
	UpdatedAt  time.Time `gorm:"column:updated_at;autoUpdateTime:false" json:"-"`
	CreatorID  *string   `gorm:"column:creator_id" json:"-"`
	TeamID     *int64    `gorm:"column:team_id" json:"-"`
	Privileges string    `gorm:"column:privileges" json:"-"`
}

func (Project) TableName() string { return "projects" }

type SchemaMigration struct {
	Version   string    `gorm:"column:version;primaryKey" json:"-"`
	AppliedAt time.Time `gorm:"column:applied_at" json:"-"`
}

func (SchemaMigration) TableName() string { return "schema_migrations" }

type TeamInvite struct {
	ID        string     `gorm:"column:id;primaryKey" json:"-"`
	TeamID    int64      `gorm:"column:team_id" json:"-"`
	TokenHash string     `gorm:"column:token_hash" json:"-"`
	CreatedBy string     `gorm:"column:created_by" json:"-"`
	CreatedAt time.Time  `gorm:"column:created_at;autoCreateTime:false" json:"-"`
	RevokedAt *time.Time `gorm:"column:revoked_at" json:"-"`
}

func (TeamInvite) TableName() string { return "team_invites" }

type TeamJoinRequest struct {
	ID         string     `gorm:"column:id;primaryKey" json:"-"`
	TeamID     int64      `gorm:"column:team_id" json:"-"`
	UserID     string     `gorm:"column:user_id" json:"-"`
	Message    string     `gorm:"column:message" json:"-"`
	Status     string     `gorm:"column:status" json:"-"`
	ReviewedBy *string    `gorm:"column:reviewed_by" json:"-"`
	CreatedAt  time.Time  `gorm:"column:created_at;autoCreateTime:false" json:"-"`
	ReviewedAt *time.Time `gorm:"column:reviewed_at" json:"-"`
}

func (TeamJoinRequest) TableName() string { return "team_join_requests" }

type TeamMember struct {
	TeamID   int64     `gorm:"column:team_id;primaryKey;autoIncrement:false" json:"-"`
	UserID   string    `gorm:"column:user_id;primaryKey" json:"-"`
	Role     string    `gorm:"column:role" json:"-"`
	JoinedAt time.Time `gorm:"column:joined_at" json:"-"`
}

func (TeamMember) TableName() string { return "team_members" }

type Team struct {
	ID              int64     `gorm:"column:id;primaryKey;autoIncrement" json:"-"`
	TeamKey         string    `gorm:"column:team_key" json:"-"`
	TeamCode        string    `gorm:"column:team_code" json:"-"`
	Name            string    `gorm:"column:name" json:"-"`
	Kind            string    `gorm:"column:kind" json:"-"`
	OwnerID         string    `gorm:"column:owner_id" json:"-"`
	PersonalOwnerID *string   `gorm:"column:personal_owner_id" json:"-"`
	Privileges      string    `gorm:"column:privileges" json:"-"`
	CreatedAt       time.Time `gorm:"column:created_at;autoCreateTime:false" json:"-"`
	UpdatedAt       time.Time `gorm:"column:updated_at;autoUpdateTime:false" json:"-"`
}

func (Team) TableName() string { return "teams" }

type User struct {
	ID           string    `gorm:"column:id;primaryKey" json:"-"`
	Account      []byte    `gorm:"column:account" json:"-"`
	PasswordHash string    `gorm:"column:password_hash" json:"-"`
	Name         string    `gorm:"column:name" json:"-"`
	Avator       string    `gorm:"column:avator" json:"-"`
	Email        string    `gorm:"column:email" json:"-"`
	Phone        string    `gorm:"column:phone" json:"-"`
	CreatedAt    time.Time `gorm:"column:created_at;autoCreateTime:false" json:"-"`
	UpdatedAt    time.Time `gorm:"column:updated_at;autoUpdateTime:false" json:"-"`
}

func (User) TableName() string { return "users" }

type WorkspaceState struct {
	ID int64 `gorm:"column:id;primaryKey;autoIncrement:false" json:"-"`
}

func (WorkspaceState) TableName() string { return "workspace_state" }
