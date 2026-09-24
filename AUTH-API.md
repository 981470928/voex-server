Current team authorization supersedes creator-only isolation; see TEAM-API.md.

# Voex authentication (experimental)

API root: /api. The Go service listens on 127.0.0.1:8090 behind Nginx.

POST /auth/account-availability {account} -> {available}
POST /auth/register {account,password,name,email?,phone?} -> {accessToken,expiresIn:900,user}
POST /auth/login {account,password} -> same session
POST /auth/refresh with HttpOnly voex_refresh cookie -> same session
POST /auth/logout with refresh cookie or Bearer access token -> {success:true}
GET /auth/me -> {id,account,name,avator,email,phone}
PATCH /auth/me {name?,email?,phone?,avator?} -> user
POST /assets/upload multipart file,path -> {id,name,url,path,size,mime,creator}
GET /assets/:id -> owned asset, or a current teammate's active avatar; cookie/Bearer required

Accounts: 8-64 Unicode codepoints, exact bytes, no trimming or case folding.
Passwords: 10-128 Unicode codepoints, Argon2id (19456 KiB, t=2, p=1).
Name: required 1-64 trimmed codepoints. Email and phone optional, unverified.
JWT: HS256, fixed issuer/audience, 15 minutes. /home/server/.secrets/jwt-key is 0600.
Refresh: random 32 bytes, database stores SHA256, fixed 14 days, revocable, no rotation.
Every request validates database session as well as JWT; logout revokes immediately.
Cookie: HttpOnly, SameSite=Strict, Secure on HTTPS; current listener is HTTP only.
AUTH_ALLOWED_ORIGINS can override trusted origins (comma separated).

Project/document/attachment access follows team and project permissions described in TEAM-API.md. Creator name/avator reflect the current profile.

Upload storage: /home/update/{path}/{server-generated-uuid}. Directories created as needed.
Each path segment: [A-Za-z0-9_-]{1,64}, relative path <=255 chars. No dots, backslash, absolute paths or symlinks.
General files <=200 MiB, avator <=5 MiB, thumbnail <=20 MiB. Images must be static JPEG/PNG/WebP <=20M pixels.
Images are decoded and re-encoded to WebP, max 512x512 avator and 1920x1920 thumbnail.
Existing document attachments stay in /home/static. Physical orphan content is retained; no scheduled cleanup is configured.

Implementation: auth.go and storage.go. Configuration, build, deployment and verification are documented in README.md.
