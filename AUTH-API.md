Current team authorization supersedes creator-only isolation; see TEAM-API.md.

# Voex authentication (experimental)

API root: /api. Node listens on 127.0.0.1:8090 behind Nginx.

POST /auth/account-availability {account} -> {available}
POST /auth/register {account,password,name,email?,phone?} -> {accessToken,expiresIn:900,user}
POST /auth/login {account,password} -> same session
POST /auth/refresh with HttpOnly voex_refresh cookie -> same session
POST /auth/logout with refresh cookie or Bearer access token -> {success:true}
GET /auth/me -> {id,account,name,avator,email,phone}
PATCH /auth/me {name?,email?,phone?,avator?} -> user
POST /assets/upload multipart file,path -> {id,name,url,path,size,mime,creator}
GET /assets/:id -> owner's file only, cookie/Bearer required

Accounts: 8-64 Unicode codepoints, exact bytes, no trimming or case folding.
Passwords: 10-128 Unicode codepoints, Argon2id (19456 KiB, t=2, p=1).
Name: required 1-64 trimmed codepoints. Email and phone optional, unverified.
JWT: HS256, fixed issuer/audience, 15 minutes. .secrets/jwt-key is 0600.
Refresh: random 32 bytes, database stores SHA256, fixed 7 days, revocable, no rotation.
Every request validates database session as well as JWT; logout revokes immediately.
Cookie: HttpOnly, SameSite=Strict, Secure on HTTPS; current listener is HTTP only.
AUTH_ALLOWED_ORIGINS can override trusted origins (comma separated).

All projects/documents/attachments/assets are creator-isolated. Creator name/avator reflect current profile.
Existing account 981470928 owns 2 projects, 4 documents, 3 attachments. No credentials stored here.

Upload storage: /home/update/{path}/{server-generated-uuid}. Directories created as needed.
Each path segment: [A-Za-z0-9_-]{1,64}, relative path <=255 chars. No dots, backslash, absolute paths or symlinks.
General files <=200 MiB, avator <=5 MiB, thumbnail <=20 MiB. Images must be static JPEG/PNG/WebP <=20M pixels.
Images are decoded and re-encoded to WebP, max 512x512 avator and 1920x1920 thumbnail.
Existing document attachments stay in /home/static. Physical orphan content is retained; no scheduled cleanup is configured.

Backup directory is recorded in .auth-backup-path; it contains source.tar.gz, database.sql, ownership.json,
api-verification.json (40 checks), browser-verification.json (12 flows) and api-smoke.cjs.
Temporary validation accounts/data were removed. No unit tests generated or run.
Local frontend is developed separately; no local code was uploaded to this server.
