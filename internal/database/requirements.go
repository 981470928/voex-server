package database

// Structural requirements accompany the explicit migration SQL. They are checked
// before adopting a legacy ledger and whenever serve/readiness checks the schema.
// Additional compatible columns are allowed; missing/wrong columns, indexes or
// foreign keys are not silently repaired or baselined.
type columnRequirement struct {
	name, typ string
	nullable  bool
	collation string
}
type indexRequirement struct {
	name, columns string
	unique        bool
}
type foreignKeyRequirement struct{ name, columns, table, referenced, deleteRule, updateRule string }
type tableRequirement struct {
	name, collation string
	columns         []columnRequirement
	indexes         []indexRequirement
	foreignKeys     []foreignKeyRequirement
	checks          []string
}

func requirementsFor(version string) []tableRequirement {
	switch version {
	case "001-workspace", "002-auth", "003-teams":
		return bootstrapRequirements
	case "004-recent-files":
		return recentFilesRequirements
	case "005-document-details":
		return documentDetailsRequirements
	case "006-trash":
		return trashRequirements
	case "007-ai-agent":
		return aiRequirements
	case "008-ai-conversation-history":
		return aiHistoryRequirements
	case "009-ai-conversation-sessions":
		return aiSessionsRequirements
	case "010-user-config":
		return userConfigRequirements
	}
	return nil
}

var bootstrapRequirements = []tableRequirement{
	{name: "assets", collation: "utf8mb4_0900_ai_ci",
		columns: []columnRequirement{
			{"id", "char(36)", false, "ascii_bin"},
			{"creator_id", "char(36)", false, "ascii_bin"},
			{"directory", "varchar(255)", false, ""},
			{"storage_name", "varchar(64)", false, ""},
			{"name", "varchar(255)", false, ""},
			{"mime", "varchar(128)", false, ""},
			{"size", "bigint unsigned", false, ""},
			{"created_at", "datetime", false, ""},
		},
		indexes: []indexRequirement{
			{"PRIMARY", "id", true},
			{"idx_asset_creator", "creator_id", false},
		},
		foreignKeys: []foreignKeyRequirement{
			{"assets_ibfk_1", "creator_id", "users", "id", "RESTRICT", "RESTRICT"},
		},
	},
	{name: "auth_sessions", collation: "utf8mb4_0900_ai_ci",
		columns: []columnRequirement{
			{"id", "char(36)", false, "ascii_bin"},
			{"user_id", "char(36)", false, "ascii_bin"},
			{"refresh_hash", "char(64)", false, "ascii_bin"},
			{"expires_at", "datetime", false, ""},
			{"created_at", "datetime", false, ""},
		},
		indexes: []indexRequirement{
			{"PRIMARY", "id", true},
			{"refresh_hash", "refresh_hash", true},
			{"idx_session_expiry", "expires_at", false},
			{"user_id", "user_id", false},
		},
		foreignKeys: []foreignKeyRequirement{
			{"auth_sessions_ibfk_1", "user_id", "users", "id", "RESTRICT", "RESTRICT"},
		},
	},
	{name: "documents", collation: "utf8mb4_0900_ai_ci",
		columns: []columnRequirement{
			{"id", "int", false, ""},
			{"file_key", "varchar(64)", false, ""},
			{"file_name", "varchar(255)", false, ""},
			{"file_content", "longtext", true, ""},
			{"created_at", "datetime", true, ""},
			{"updated_at", "datetime", true, ""},
			{"folder_id", "int", false, ""},
			{"creator_id", "char(36)", true, "ascii_bin"},
			{"privileges", "json", false, ""},
			{"revision", "int unsigned", false, ""},
		},
		indexes: []indexRequirement{
			{"PRIMARY", "id", true},
			{"file_key", "file_key", true},
			{"idx_file_key", "file_key", false},
			{"idx_documents_folder", "folder_id,created_at,id", false},
			{"idx_documents_creator", "creator_id", false},
		},
		foreignKeys: []foreignKeyRequirement{
			{"fk_documents_creator", "creator_id", "users", "id", "RESTRICT", "RESTRICT"},
			{"fk_documents_folder", "folder_id", "folders", "id", "RESTRICT", "RESTRICT"},
		},
	},
	{name: "file_shares", collation: "utf8mb4_0900_ai_ci",
		columns: []columnRequirement{
			{"id", "char(36)", false, "ascii_bin"},
			{"document_id", "int", false, ""},
			{"token_hash", "char(64)", false, "ascii_bin"},
			{"permission", "enum('read','edit')", false, ""},
			{"created_by", "char(36)", false, "ascii_bin"},
			{"created_at", "datetime", false, ""},
			{"revoked_at", "datetime", true, ""},
		},
		indexes: []indexRequirement{
			{"PRIMARY", "id", true},
			{"token_hash", "token_hash", true},
			{"document_id", "document_id", false},
			{"created_by", "created_by", false},
		},
		foreignKeys: []foreignKeyRequirement{
			{"file_shares_ibfk_1", "document_id", "documents", "id", "CASCADE", "RESTRICT"},
			{"file_shares_ibfk_2", "created_by", "users", "id", "RESTRICT", "RESTRICT"},
		},
	},
	{name: "files", collation: "utf8mb4_0900_ai_ci",
		columns: []columnRequirement{
			{"id", "int", false, ""},
			{"file_key", "varchar(64)", false, ""},
			{"hash", "varchar(128)", false, ""},
			{"name", "varchar(255)", false, ""},
			{"mime", "varchar(128)", false, ""},
			{"size", "bigint", false, ""},
			{"created_at", "datetime", true, ""},
			{"creator_id", "char(36)", true, "ascii_bin"},
			{"privileges", "json", false, ""},
		},
		indexes: []indexRequirement{
			{"PRIMARY", "id", true},
			{"idx_file_key", "file_key", false},
			{"idx_hash", "hash", false},
			{"idx_files_creator", "creator_id", false},
		},
		foreignKeys: []foreignKeyRequirement{
			{"fk_files_creator", "creator_id", "users", "id", "RESTRICT", "RESTRICT"},
		},
	},
	{name: "folders", collation: "utf8mb4_0900_ai_ci",
		columns: []columnRequirement{
			{"id", "int", false, ""},
			{"folder_key", "varchar(64)", false, "utf8mb4_bin"},
			{"project_id", "int", false, ""},
			{"parent_id", "int", true, ""},
			{"name", "varchar(255)", false, ""},
			{"created_at", "datetime", false, ""},
			{"updated_at", "datetime", false, ""},
			{"privileges", "json", false, ""},
		},
		indexes: []indexRequirement{
			{"PRIMARY", "id", true},
			{"folder_key", "folder_key", true},
			{"uq_folders_project_id", "project_id,id", true},
			{"idx_folders_parent", "project_id,parent_id,created_at,id", false},
			{"idx_folders_order", "project_id,created_at,id", false},
		},
		foreignKeys: []foreignKeyRequirement{
			{"fk_folders_parent", "project_id,parent_id", "folders", "project_id,id", "RESTRICT", "RESTRICT"},
			{"fk_folders_project", "project_id", "projects", "id", "RESTRICT", "RESTRICT"},
		},
		checks: []string{"chk_folders_flat"},
	},
	{name: "project_members", collation: "utf8mb4_0900_ai_ci",
		columns: []columnRequirement{
			{"project_id", "int", false, ""},
			{"user_id", "char(36)", false, "ascii_bin"},
		},
		indexes: []indexRequirement{
			{"PRIMARY", "project_id,user_id", true},
			{"user_id", "user_id", false},
		},
		foreignKeys: []foreignKeyRequirement{
			{"project_members_ibfk_1", "project_id", "projects", "id", "CASCADE", "RESTRICT"},
			{"project_members_ibfk_2", "user_id", "users", "id", "RESTRICT", "RESTRICT"},
		},
	},
	{name: "projects", collation: "utf8mb4_0900_ai_ci",
		columns: []columnRequirement{
			{"id", "int", false, ""},
			{"project_key", "varchar(64)", false, "utf8mb4_bin"},
			{"name", "varchar(255)", false, ""},
			{"created_at", "datetime", false, ""},
			{"updated_at", "datetime", false, ""},
			{"creator_id", "char(36)", true, "ascii_bin"},
			{"team_id", "int", true, ""},
			{"privileges", "json", false, ""},
		},
		indexes: []indexRequirement{
			{"PRIMARY", "id", true},
			{"project_key", "project_key", true},
			{"idx_projects_order", "created_at,id", false},
			{"idx_projects_creator", "creator_id", false},
			{"idx_projects_team", "team_id", false},
		},
		foreignKeys: []foreignKeyRequirement{
			{"fk_projects_creator", "creator_id", "users", "id", "RESTRICT", "RESTRICT"},
			{"fk_projects_team", "team_id", "teams", "id", "RESTRICT", "RESTRICT"},
		},
	},
	{name: "team_invites", collation: "utf8mb4_0900_ai_ci",
		columns: []columnRequirement{
			{"id", "char(36)", false, "ascii_bin"},
			{"team_id", "int", false, ""},
			{"token_hash", "char(64)", false, "ascii_bin"},
			{"created_by", "char(36)", false, "ascii_bin"},
			{"created_at", "datetime", false, ""},
			{"revoked_at", "datetime", true, ""},
		},
		indexes: []indexRequirement{
			{"PRIMARY", "id", true},
			{"token_hash", "token_hash", true},
			{"team_id", "team_id", false},
			{"created_by", "created_by", false},
		},
		foreignKeys: []foreignKeyRequirement{
			{"team_invites_ibfk_1", "team_id", "teams", "id", "CASCADE", "RESTRICT"},
			{"team_invites_ibfk_2", "created_by", "users", "id", "RESTRICT", "RESTRICT"},
		},
	},
	{name: "team_join_requests", collation: "utf8mb4_0900_ai_ci",
		columns: []columnRequirement{
			{"id", "char(36)", false, "ascii_bin"},
			{"team_id", "int", false, ""},
			{"user_id", "char(36)", false, "ascii_bin"},
			{"message", "varchar(1000)", false, ""},
			{"status", "enum('pending','approved','rejected')", false, ""},
			{"reviewed_by", "char(36)", true, "ascii_bin"},
			{"created_at", "datetime", false, ""},
			{"reviewed_at", "datetime", true, ""},
		},
		indexes: []indexRequirement{
			{"PRIMARY", "id", true},
			{"uq_team_request", "team_id,user_id", true},
			{"user_id", "user_id", false},
			{"reviewed_by", "reviewed_by", false},
		},
		foreignKeys: []foreignKeyRequirement{
			{"team_join_requests_ibfk_1", "team_id", "teams", "id", "CASCADE", "RESTRICT"},
			{"team_join_requests_ibfk_2", "user_id", "users", "id", "RESTRICT", "RESTRICT"},
			{"team_join_requests_ibfk_3", "reviewed_by", "users", "id", "RESTRICT", "RESTRICT"},
		},
	},
	{name: "team_members", collation: "utf8mb4_0900_ai_ci",
		columns: []columnRequirement{
			{"team_id", "int", false, ""},
			{"user_id", "char(36)", false, "ascii_bin"},
			{"role", "enum('admin','member')", false, ""},
			{"joined_at", "datetime", false, ""},
		},
		indexes: []indexRequirement{
			{"PRIMARY", "team_id,user_id", true},
			{"user_id", "user_id", false},
		},
		foreignKeys: []foreignKeyRequirement{
			{"team_members_ibfk_1", "team_id", "teams", "id", "CASCADE", "RESTRICT"},
			{"team_members_ibfk_2", "user_id", "users", "id", "RESTRICT", "RESTRICT"},
		},
	},
	{name: "teams", collation: "utf8mb4_0900_ai_ci",
		columns: []columnRequirement{
			{"id", "int", false, ""},
			{"team_key", "varchar(64)", false, "utf8mb4_bin"},
			{"team_code", "char(10)", false, "ascii_bin"},
			{"name", "varchar(128)", false, ""},
			{"kind", "enum('personal','standard')", false, ""},
			{"owner_id", "char(36)", false, "ascii_bin"},
			{"personal_owner_id", "char(36)", true, "ascii_bin"},
			{"privileges", "json", false, ""},
			{"created_at", "datetime", false, ""},
			{"updated_at", "datetime", false, ""},
		},
		indexes: []indexRequirement{
			{"PRIMARY", "id", true},
			{"team_key", "team_key", true},
			{"team_code", "team_code", true},
			{"personal_owner_id", "personal_owner_id", true},
			{"owner_id", "owner_id", false},
		},
		foreignKeys: []foreignKeyRequirement{
			{"teams_ibfk_1", "owner_id", "users", "id", "RESTRICT", "RESTRICT"},
			{"teams_ibfk_2", "personal_owner_id", "users", "id", "RESTRICT", "RESTRICT"},
		},
		checks: []string{"teams_chk_1"},
	},
	{name: "users", collation: "utf8mb4_0900_ai_ci",
		columns: []columnRequirement{
			{"id", "char(36)", false, "ascii_bin"},
			{"account", "varbinary(256)", false, ""},
			{"password_hash", "varchar(255)", false, ""},
			{"name", "varchar(64)", false, ""},
			{"avator", "varchar(255)", false, ""},
			{"email", "varchar(254)", false, ""},
			{"phone", "varchar(32)", false, ""},
			{"created_at", "datetime", false, ""},
			{"updated_at", "datetime", false, ""},
		},
		indexes: []indexRequirement{
			{"PRIMARY", "id", true},
			{"account", "account", true},
		},
	},
	{name: "workspace_state", collation: "utf8mb4_0900_ai_ci",
		columns: []columnRequirement{
			{"id", "tinyint", false, ""},
		},
		indexes: []indexRequirement{
			{"PRIMARY", "id", true},
		},
	},
}

var recentFilesRequirements = []tableRequirement{
	{name: "recent_files", collation: "utf8mb4_0900_ai_ci",
		columns: []columnRequirement{
			{"user_id", "char(36)", false, "ascii_bin"},
			{"document_id", "int", false, ""},
			{"share_id", "char(36)", true, "ascii_bin"},
			{"file_name", "varchar(255)", false, ""},
			{"viewed_at", "datetime(6)", false, ""},
		},
		indexes: []indexRequirement{
			{"PRIMARY", "user_id,document_id", true},
			{"idx_recent_files_order", "user_id,viewed_at,document_id", false},
			{"idx_recent_files_document", "document_id", false},
			{"idx_recent_files_share", "share_id", false},
		},
		foreignKeys: []foreignKeyRequirement{
			{"fk_recent_files_user", "user_id", "users", "id", "CASCADE", "RESTRICT"},
			{"fk_recent_files_document", "document_id", "documents", "id", "CASCADE", "RESTRICT"},
			{"fk_recent_files_share", "share_id", "file_shares", "id", "SET NULL", "RESTRICT"},
		},
	},
}

var documentDetailsRequirements = []tableRequirement{
	{name: "documents", collation: "",
		columns: []columnRequirement{
			{"updated_by", "char(36)", true, "ascii_bin"},
			{"updated_by_anonymous", "tinyint(1)", false, ""},
		},
		indexes: []indexRequirement{
			{"idx_documents_updated_by", "updated_by", false},
		},
		foreignKeys: []foreignKeyRequirement{
			{"fk_documents_updated_by", "updated_by", "users", "id", "RESTRICT", "RESTRICT"},
		},
	},
}

var trashRequirements = []tableRequirement{
	{name: "documents", collation: "",
		columns: []columnRequirement{
			{"deleted_at", "datetime(6)", true, ""},
			{"deleted_by", "char(36)", true, "ascii_bin"},
			{"expires_at", "datetime(6)", true, ""},
		},
		indexes: []indexRequirement{
			{"idx_documents_trash_expiry", "expires_at,id", false},
			{"idx_documents_folder_deleted", "folder_id,deleted_at,created_at,id", false},
			{"idx_documents_deleted_by", "deleted_by", false},
		},
		foreignKeys: []foreignKeyRequirement{
			{"fk_documents_deleted_by", "deleted_by", "users", "id", "RESTRICT", "RESTRICT"},
		},
	},
	{name: "folders", collation: "",
		columns: []columnRequirement{
			{"deleted_at", "datetime(6)", true, ""},
			{"deleted_by", "char(36)", true, "ascii_bin"},
			{"expires_at", "datetime(6)", true, ""},
		},
		indexes: []indexRequirement{
			{"idx_folders_trash_expiry", "expires_at,id", false},
			{"idx_folders_project_deleted", "project_id,deleted_at,created_at,id", false},
			{"idx_folders_deleted_by", "deleted_by", false},
		},
		foreignKeys: []foreignKeyRequirement{
			{"fk_folders_deleted_by", "deleted_by", "users", "id", "RESTRICT", "RESTRICT"},
		},
	},
}

var aiRequirements = []tableRequirement{
	{name: "ai_models", collation: "utf8mb4_unicode_ci",
		columns: []columnRequirement{
			{"id", "varchar(64)", false, "ascii_bin"},
			{"user_id", "char(36)", false, "ascii_bin"},
			{"name", "varchar(128)", false, ""},
			{"protocol", "varchar(16)", false, ""},
			{"base_url", "varchar(2048)", false, ""},
			{"model", "varchar(255)", false, ""},
			{"key_cipher", "text", false, ""},
			{"temperature", "double", true, ""},
			{"max_tokens", "int unsigned", false, ""},
			{"token_parameter", "varchar(24)", false, ""},
			{"created_at", "datetime", false, ""},
		},
		indexes: []indexRequirement{
			{"PRIMARY", "id", true},
			{"idx_ai_model_user", "user_id", false},
		},
		foreignKeys: []foreignKeyRequirement{
			{"fk_ai_model_user", "user_id", "users", "id", "CASCADE", "RESTRICT"},
		},
	},
	{name: "ai_review_applications", collation: "utf8mb4_unicode_ci",
		columns: []columnRequirement{
			{"review_id", "varchar(64)", false, "ascii_bin"},
			{"user_id", "char(36)", false, "ascii_bin"},
			{"payload_hash", "char(64)", false, "ascii_bin"},
			{"result_json", "json", false, ""},
			{"created_at", "datetime", false, ""},
		},
		indexes: []indexRequirement{
			{"PRIMARY", "user_id,review_id", true},
		},
		foreignKeys: []foreignKeyRequirement{
			{"fk_ai_review_user", "user_id", "users", "id", "CASCADE", "RESTRICT"},
		},
	},
}

var aiHistoryRequirements = []tableRequirement{
	{name: "ai_conversation_turns", collation: "utf8mb4_unicode_ci",
		columns: []columnRequirement{
			{"id", "bigint unsigned", false, ""},
			{"user_id", "char(36)", false, "ascii_bin"},
			{"document_id", "int", false, ""},
			{"turn_id", "varchar(64)", false, "ascii_bin"},
			{"model_id", "varchar(64)", false, "ascii_bin"},
			{"prompt", "longtext", false, ""},
			{"response", "longtext", false, ""},
			{"activity", "json", false, ""},
			{"error", "text", false, ""},
			{"review_id", "varchar(64)", true, "ascii_bin"},
			{"review_json", "json", true, ""},
			{"finish_hash", "char(64)", true, "ascii_bin"},
			{"created_at", "datetime(3)", false, ""},
			{"completed_at", "datetime(3)", true, ""},
		},
		indexes: []indexRequirement{
			{"PRIMARY", "id", true},
			{"uq_ai_turn", "user_id,turn_id", true},
			{"uq_ai_turn_review", "user_id,review_id", true},
			{"idx_ai_conversation", "user_id,document_id,id", false},
		},
		foreignKeys: []foreignKeyRequirement{
			{"fk_ai_turn_user", "user_id", "users", "id", "CASCADE", "RESTRICT"},
			{"fk_ai_turn_document", "document_id", "documents", "id", "CASCADE", "RESTRICT"},
		},
	},
}

var aiSessionsRequirements = []tableRequirement{
	{name: "ai_conversation_sessions", collation: "utf8mb4_unicode_ci",
		columns: []columnRequirement{
			{"id", "bigint unsigned", false, ""},
			{"user_id", "char(36)", false, "ascii_bin"},
			{"document_id", "int", false, ""},
			{"session_id", "varchar(64)", false, "ascii_bin"},
			{"title", "varchar(80)", false, ""},
			{"created_at", "datetime(3)", false, ""},
			{"updated_at", "datetime(3)", false, ""},
		},
		indexes: []indexRequirement{
			{"PRIMARY", "id", true},
			{"uq_ai_session", "user_id,document_id,session_id", true},
			{"idx_ai_sessions", "user_id,document_id,id", false},
		},
		foreignKeys: []foreignKeyRequirement{
			{"fk_ai_session_user", "user_id", "users", "id", "CASCADE", "RESTRICT"},
			{"fk_ai_session_document", "document_id", "documents", "id", "CASCADE", "RESTRICT"},
		},
	},
	{name: "ai_conversation_turns", collation: "",
		columns: []columnRequirement{
			{"session_id", "varchar(64)", false, "ascii_bin"},
		},
		indexes: []indexRequirement{
			{"idx_ai_session_turns", "user_id,document_id,session_id,id", false},
		},
	},
	{name: "ai_conversation_turns", collation: "",
		foreignKeys: []foreignKeyRequirement{
			{"fk_ai_turn_session", "user_id,document_id,session_id", "ai_conversation_sessions", "user_id,document_id,session_id", "CASCADE", "RESTRICT"},
		},
	},
}

var userConfigRequirements = []tableRequirement{
	{name: "users", columns: []columnRequirement{
		{"user_config", "json", false, ""},
	}},
}
