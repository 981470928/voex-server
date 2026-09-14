# Teams and sharing (experimental)

Current contract: users join multiple teams as owner/admin/member. Each user has exactly one personal team, created transactionally with registration. Personal teams allow members and projects, but cannot be deleted or transferred; their owner cannot leave.

Teams -> projects -> one level of folders -> documents. Migration 003-teams adds team ownership, memberships, privileges JSON, project membership lists, join requests, invitations, shares, and document revision. Existing nested folders cause migration to stop rather than move data automatically.

Owner/admin always edit every project. Members edit inherit-mode projects or projects listing them explicitly. An empty restricted list grants access only to owner/admin. Every team member can create a project; creating folders/documents requires edit permission in that project. Creator remains authorship metadata.

privileges describes policy; permissions describes effective request capabilities. Folders always inherit. Project privilege settings require owner/admin. Project editors can rename/delete empty projects. Team ownership transfer is owner-only, standard teams only; previous owner becomes admin.

/api/teams: GET list, POST create.
/api/teams/:key: GET, PATCH name, DELETE empty standard team (owner).
/api/teams/:key/members: GET; /:userId PATCH role (owner), DELETE remove/leave.
/api/teams/:key/transfer: POST {user_id}.
/api/teams/:key/invites: GET/POST; /:id DELETE revoke.
/api/team-invites/inspect: POST {token}, public minimal team preview.
/api/team-join-requests: POST {team_code or invite_token,message}, GET own requests.
/api/teams/:key/join-requests: GET; /:id PATCH {status:approved or rejected}.
Applications use 1-1000 trimmed Unicode codepoints. Pending submissions are idempotent. Reapplying after a processed request generates a new id to prevent stale approval.

/projects?team_key=... lists authorized projects in selected team. /project POST {name,team_key}. /project/:key/privileges GET/PUT {mode:inherit or restricted,user_ids:[]}.
/document/:key PUT content requires current revision; only content changes increment it. Revision mismatch is HTTP409. Name-only changes keep revision.
/document/:key/shares GET/POST {permission:read or edit}; /:id DELETE revoke. Only project members may create shares; managers revoke any, ordinary members revoke own.

Browser links use /share#TOKEN or /join#TOKEN. Tokens are generated from 32 random bytes, stored only as SHA256 hashes. Invitation links still require application and review.
/shared-file GET/PUT uses X-Share-Token header and optional JWT. Any valid link holder can access that document only. Edit links allow anonymous content edits. Project edit rights override read links. Public PUT only accepts file_content and revision. No team/project/folder identifiers or contact details are exposed.
/shared-file/attachments/:hash GET binds file_key+hash to the shared document. /shared-file/creator-avator is capability-scoped. Revocation takes effect on the next request. General /assets/upload and document /upload still require JWT; links never grant general upload or project access.

Backup and verification directory is recorded in .team-backup-path. team-api-verification.json:108 API checks; focused-verification.json:6 checks; team-final-verification.json records browser scope and cleanup. Temporary accounts, teams, files and links were removed. Original 2 projects, 4 documents, 3 attachments remain in the original user's personal team.

Invitation URL opening was blocked by browser safety policy; the API and team-number application/review UI were verified without circumventing it. Frontend was implemented locally and not uploaded. Server remains HTTP-only; TLS is outside this changeset. No unit tests were generated or run.
