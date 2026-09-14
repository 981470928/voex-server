import type { RowDataPacket } from "mysql2/promise";

export type ProjectPermissions = {
  read: boolean;
  write: boolean;
  manage: boolean;
  share: boolean;
};

export type Project = {
  team_key: string;
  privileges: { mode: string };
  project_key: string;
  name: string;
  created_at: string;
  updated_at: string;
  permissions: ProjectPermissions;
};

export type Folder = {
  privileges: { mode: string };
  folder_key: string;
  project_key: string;
  parent_key: string | null;
  name: string;
  created_at: string;
  updated_at: string;
};

export type Creator = { id: string; name: string; avator: string };

export type DocumentSummary = {
  revision: number;
  privileges: { mode: string };
  creator: Creator;
  id: number;
  file_key: string;
  file_name: string;
  project_key: string;
  folder_key: string;
  created_at: string;
  updated_at: string;
};

export type DocumentInfo = DocumentSummary & { file_content: string | null };

export interface ProjectRow extends RowDataPacket {
  team_id: number;
  team_key: string;
  privileges: { mode: string } | string;
  id: number;
  project_key: string;
  name: string;
  created_at: Date | string;
  updated_at: Date | string;
}

export interface FolderRow extends RowDataPacket {
  id: number;
  folder_key: string;
  project_id: number;
  parent_id: number | null;
  name: string;
  created_at: Date | string;
  updated_at: Date | string;
}

export interface DocumentRow extends RowDataPacket {
  revision: number;
  team_key: string;
  creator_id: string;
  creator_name: string;
  creator_avator: string;
  id: number;
  file_key: string;
  file_name: string;
  project_id: number;
  project_key: string;
  folder_key: string;
  created_at: Date | string;
  updated_at: Date | string;
  file_content?: string | null;
}

export function timestamp(value: Date | string): string {
  return value instanceof Date ? value.toISOString() : value;
}

export function projectDto(
  row: ProjectRow,
  permissions: ProjectPermissions,
): Project {
  return {
    project_key: row.project_key,
    team_key: row.team_key,
    privileges:
      typeof row.privileges === "string"
        ? JSON.parse(row.privileges)
        : row.privileges,
    name: row.name,
    created_at: timestamp(row.created_at),
    updated_at: timestamp(row.updated_at),
    permissions,
  };
}

export function folderDto(
  row: FolderRow,
  projectKey: string,
  parentKey: string | null,
): Folder {
  return {
    folder_key: row.folder_key,
    privileges: { mode: "inherit" },
    project_key: projectKey,
    parent_key: parentKey,
    name: row.name,
    created_at: timestamp(row.created_at),
    updated_at: timestamp(row.updated_at),
  };
}

export function documentDto(row: DocumentRow): DocumentSummary {
  return {
    revision: Number(row.revision),
    privileges: { mode: "inherit" },
    creator: {
      id: row.creator_id,
      name: row.creator_name,
      avator: row.creator_avator,
    },
    id: row.id,
    file_key: row.file_key,
    file_name: row.file_name,
    project_key: row.project_key,
    folder_key: row.folder_key,
    created_at: timestamp(row.created_at),
    updated_at: timestamp(row.updated_at),
  };
}
