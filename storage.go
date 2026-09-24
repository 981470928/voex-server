package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

var staticDir = storageEnv("VOEX_STATIC_DIR", "/home/static")
var uploadDir = storageEnv("VOEX_UPLOAD_DIR", "/home/update")
var tempDir = filepath.Join(uploadDir, ".incoming")
var directoryPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}(/[A-Za-z0-9_-]{1,64})*$`)
var attachmentHashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var assetStoragePattern = regexp.MustCompile(`^[a-f0-9-]{36}(\.webp)?$`)
var assetMimePattern = regexp.MustCompile(`^[a-zA-Z0-9.+-]+/[a-zA-Z0-9.+-]+$`)

func storageEnv(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return filepath.Clean(value)
	}
	return fallback
}

func initializeStorage() {
	staticDir = storageEnv("VOEX_STATIC_DIR", "/home/static")
	uploadDir = storageEnv("VOEX_UPLOAD_DIR", "/home/update")
	tempDir = filepath.Join(uploadDir, ".incoming")
	for _, directory := range []string{staticDir, uploadDir, tempDir} {
		if !filepath.IsAbs(directory) {
			panic("Storage directory must be absolute")
		}
		must(os.MkdirAll(directory, 0700))
		info, err := os.Lstat(directory)
		must(err)
		resolved, err := filepath.EvalSymlinks(directory)
		must(err)
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || resolved != directory {
			panic("Storage directory must not be a symbolic link")
		}
	}
}

func safeDirectory(value any) string {
	directory, ok := value.(string)
	if !ok || len(directory) > 255 || !directoryPattern.MatchString(directory) {
		fail(400, "path 只能包含字母、数字、短横线、下划线和分层斜杠")
	}
	return directory
}

func storageDirectory(directory string) string {
	safeDirectory(directory)
	resolved := uploadDir
	for _, part := range strings.Split(directory, "/") {
		resolved = filepath.Join(resolved, part)
		err := os.Mkdir(resolved, 0700)
		if err != nil && !os.IsExist(err) {
			must(err)
		}
		info, err := os.Lstat(resolved)
		actual, realErr := filepath.EvalSymlinks(resolved)
		if err != nil || realErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || actual != resolved {
			fail(400, "上传目录不合法")
		}
	}
	return resolved
}

type incomingUpload struct {
	Path   string
	Name   string
	MIME   string
	Size   int64
	Fields Row
}

// Read the multipart stream with the same file, field, and size limits as the previous API.
func readUpload(w http.ResponseWriter, r *http.Request, asset bool) (upload incomingUpload) {
	const maximum = 200 * 1024 * 1024
	upload.Fields = Row{}
	complete := false
	defer func() {
		if !complete && upload.Path != "" {
			_ = os.Remove(upload.Path)
		}
	}()
	r.Body = http.MaxBytesReader(w, r.Body, maximum+1024*1024)
	reader, err := r.MultipartReader()
	if err != nil {
		fail(400, "请求格式错误，请检查上传表单")
	}
	maxFields, maxParts, maxFieldSize, maxNameSize := 3, 5, int64(4096), 100
	if asset {
		maxFields, maxParts, maxFieldSize, maxNameSize = 1, 3, 255, 30
	}
	fields, parts := 0, 0
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			uploadReadError(err)
		}
		parts++
		name := part.FormName()
		if parts > maxParts || len(name) > maxNameSize || name == "" {
			fail(400, "上传字段或文件数量不合法")
		}
		if part.FileName() == "" {
			fields++
			if fields > maxFields {
				fail(400, "上传字段或文件数量不合法")
			}
			data, err := io.ReadAll(io.LimitReader(part, maxFieldSize+1))
			if err != nil {
				uploadReadError(err)
			}
			if int64(len(data)) > maxFieldSize {
				fail(400, "上传字段或文件数量不合法")
			}
			if _, exists := upload.Fields[name]; exists {
				fail(400, "上传字段或文件数量不合法")
			}
			upload.Fields[name] = string(data)
		} else {
			if name != "file" || upload.Path != "" {
				fail(400, "上传字段或文件数量不合法")
			}
			file, err := os.CreateTemp(tempDir, "upload-")
			must(err)
			upload.Path, upload.Name, upload.MIME = file.Name(), part.FileName(), part.Header.Get("Content-Type")
			upload.Size, err = io.Copy(file, io.LimitReader(part, maximum+1))
			closeErr := file.Close()
			if err != nil {
				uploadReadError(err)
			}
			must(closeErr)
			if upload.Size > maximum {
				fail(413, "上传文件超过大小限制")
			}
		}
		if err := part.Close(); err != nil {
			uploadReadError(err)
		}
	}
	complete = true
	return upload
}

func uploadReadError(err error) {
	var oversized *http.MaxBytesError
	if errors.As(err, &oversized) {
		fail(413, "上传文件超过大小限制")
	}
	var pathError *os.PathError
	if errors.As(err, &pathError) {
		must(err)
	}
	fail(400, "请求格式错误，请检查上传表单")
}

func computeFileHash(filename string) string {
	file, err := os.Open(filename)
	must(err)
	defer file.Close()
	hash := sha256.New()
	_, err = io.Copy(hash, file)
	must(err)
	return hex.EncodeToString(hash.Sum(nil))
}

func copyStorageFile(source, destination string, reuse bool) {
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if reuse && os.IsExist(err) {
		return
	}
	must(err)
	complete := false
	defer func() {
		_ = output.Close()
		if !complete {
			_ = os.Remove(destination)
		}
	}()
	input, err := os.Open(source)
	must(err)
	defer input.Close()
	_, err = io.Copy(output, input)
	must(err)
	must(output.Close())
	complete = true
}

func imageMetadata(ctx context.Context, filename string) (map[string]string, error) {
	output, err := exec.CommandContext(ctx, "vipsheader", "-a", filename).Output()
	if err != nil {
		return nil, err
	}
	metadata := map[string]string{}
	for _, line := range strings.Split(string(output), "\n") {
		key, value, found := strings.Cut(line, ":")
		if found {
			metadata[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return metadata, nil
}

// libvips exposes page counts for WebP. Inspect animation chunks as well so animated
// PNG/WebP cannot silently become static images on a loader without animation support.
func staticImage(filename string, loader string) bool {
	file, err := os.Open(filename)
	if err != nil {
		return false
	}
	defer file.Close()
	if loader == "jpegload" {
		return true
	}
	header := make([]byte, 12)
	if loader == "pngload" {
		if _, err = io.ReadFull(file, header[:8]); err != nil || string(header[:8]) != "\x89PNG\r\n\x1a\n" {
			return false
		}
		for {
			if _, err = io.ReadFull(file, header[:8]); err != nil {
				return false
			}
			size := int64(binary.BigEndian.Uint32(header[:4]))
			kind := string(header[4:8])
			if kind == "acTL" {
				return false
			}
			if kind == "IEND" {
				return true
			}
			if _, err = io.CopyN(io.Discard, file, size+4); err != nil {
				return false
			}
		}
	}
	if loader == "webpload" {
		if _, err = io.ReadFull(file, header); err != nil || string(header[:4]) != "RIFF" || string(header[8:12]) != "WEBP" {
			return false
		}
		for {
			_, err = io.ReadFull(file, header[:8])
			if errors.Is(err, io.EOF) {
				return true
			}
			if err != nil {
				return false
			}
			size := int64(binary.LittleEndian.Uint32(header[4:8]))
			if string(header[:4]) == "ANIM" || string(header[:4]) == "ANMF" {
				return false
			}
			if _, err = io.CopyN(io.Discard, file, size+(size%2)); err != nil {
				return false
			}
		}
	}
	return false
}

func convertStorageImage(r *http.Request, source, destination, directory string) error {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	metadata, err := imageMetadata(ctx, source)
	if err != nil {
		return err
	}
	width, widthErr := strconv.ParseInt(metadata["width"], 10, 64)
	height, heightErr := strconv.ParseInt(metadata["height"], 10, 64)
	if widthErr != nil || heightErr != nil || width < 1 || height < 1 || width > 20000000/height {
		return fmt.Errorf("invalid image dimensions")
	}
	if pages := metadata["n-pages"]; pages != "" && pages != "1" {
		return fmt.Errorf("animated image")
	}
	if !staticImage(source, metadata["vips-loader"]) {
		return fmt.Errorf("unsupported image")
	}
	maximum := "1920"
	if directory == "avator" {
		maximum = "512"
	}
	// thumbnail autorotates EXIF orientation and size=down forbids enlargement.
	return exec.CommandContext(ctx, "vips", "thumbnail", source+"[fail-on=error]", destination+"[Q=85,strip]", maximum, "--height", maximum, "--size", "down").Run()
}

func storageFile(w http.ResponseWriter, r *http.Request, filename, name, mimeType string, inline bool, cache, missing string) {
	// O_NOFOLLOW closes the lstat/open race and never serves a symlink target.
	file, err := os.OpenFile(filename, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		fail(404, missing)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		fail(404, missing)
	}
	disposition := "attachment"
	if inline {
		disposition = "inline"
	}
	encoded := strings.ReplaceAll(url.QueryEscape(name), "+", "%20")
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Cache-Control", cache)
	w.Header().Set("Content-Disposition", disposition+"; filename*=UTF-8''"+encoded)
	http.ServeContent(w, r, name, info.ModTime(), file)
}

func attachmentHash(value string) string {
	if !attachmentHashPattern.MatchString(value) {
		fail(400, "附件标识不合法")
	}
	return value
}

func (s *Server) attachment(c DBTX, r *http.Request) (string, string, Row) {
	fileKey := keyValue(r.PathValue("fileKey"), "文档标识")
	hash := attachmentHash(r.PathValue("hash"))
	action := "write"
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		action = "read"
	}
	s.findDocument(c, r, fileKey, action, false)
	file := one(c, "SELECT id,hash,name,mime,size FROM files WHERE file_key=? AND hash=?", fileKey, hash)
	if file == nil {
		fail(404, "附件不存在")
	}
	return fileKey, hash, file
}

func (s *Server) registerStorageRoutes() {
	s.route("POST /api/assets/upload", "required", func(w http.ResponseWriter, r *http.Request) {
		user := currentUser(r)
		file := readUpload(w, r, true)
		if file.Path != "" {
			defer os.Remove(file.Path)
		}
		if file.Path == "" || file.Size == 0 {
			fail(400, "请选择非空文件")
		}
		directory := safeDirectory(file.Fields["path"])
		destination := storageDirectory(directory)
		id := uuid()
		filename, mimeType, size := id, "application/octet-stream", file.Size
		finalPath, committed := "", false
		defer func() {
			if !committed && finalPath != "" {
				_ = os.Remove(finalPath)
			}
		}()
		if directory == "avator" || directory == "thumbnail" {
			maximum := int64(20)
			if directory == "avator" {
				maximum = 5
			}
			if file.Size > maximum*1024*1024 {
				fail(413, fmt.Sprintf("图片不能超过 %d MB", maximum))
			}
			filename += ".webp"
			finalPath = filepath.Join(destination, filename)
			if err := convertStorageImage(r, file.Path, finalPath, directory); err != nil {
				fail(400, "请选择有效的静态 PNG、JPEG 或 WebP 图片（最多 2000 万像素）")
			}
			info, err := os.Stat(finalPath)
			must(err)
			mimeType, size = "image/webp", info.Size()
		} else {
			finalPath = filepath.Join(destination, filename)
			copyStorageFile(file.Path, finalPath, false)
		}
		name := []rune(file.Name)
		if len(name) > 255 {
			name = name[:255]
		}
		execSQL(s.db, "INSERT INTO assets (id,creator_id,directory,storage_name,name,mime,size) VALUES (?,?,?,?,?,?,?)", id, user["id"], directory, filename, string(name), mimeType, size)
		committed = true
		jsonResponse(w, 201, Row{"id": id, "name": string(name), "url": "/api/assets/" + id, "path": directory, "size": size, "mime": mimeType, "creator": creator(user)})
	})
	s.route("GET /api/assets/{id}", "required", func(w http.ResponseWriter, r *http.Request) {
		user := currentUser(r)
		asset := one(s.db, `SELECT a.* FROM assets a WHERE a.id=? AND (a.creator_id=? OR (a.directory='avator' AND EXISTS (SELECT 1 FROM users u JOIN team_members owner_member ON owner_member.user_id=u.id JOIN team_members viewer_member ON viewer_member.team_id=owner_member.team_id WHERE u.id=a.creator_id AND u.avator=CONCAT('/api/assets/',a.id) AND viewer_member.user_id=?)))`, r.PathValue("id"), user["id"], user["id"])
		if asset == nil || !assetStoragePattern.MatchString(str(asset["storage_name"])) {
			fail(404, "文件不存在")
		}
		filename := filepath.Join(storageDirectory(str(asset["directory"])), str(asset["storage_name"]))
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		storageFile(w, r, filename, str(asset["name"]), str(asset["mime"]), str(asset["mime"]) == "image/webp", "private, no-store", "文件不存在")
	})
	s.route("POST /api/upload", "required", func(w http.ResponseWriter, r *http.Request) {
		user := currentUser(r)
		file := readUpload(w, r, false)
		if file.Path != "" {
			defer os.Remove(file.Path)
		}
		if file.Path == "" {
			fail(400, "请选择文件")
		}
		fileKey := keyValue(file.Fields["fileKey"], "文档标识")
		s.tx(false, func(c *sql.Tx) any { return s.findDocument(c, r, fileKey, "write", false) })
		hash := computeFileHash(file.Path)
		name, mimeType := file.Name, file.MIME
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		if value, ok := file.Fields["file_name"]; ok {
			name = str(value)
		}
		if value, ok := file.Fields["mime"]; ok {
			mimeType = str(value)
		}
		if name == "" || utf8.RuneCountInString(name) > 255 || len(mimeType) > 128 || !assetMimePattern.MatchString(mimeType) {
			fail(400, "文件名或文件类型不合法")
		}
		s.tx(true, func(c *sql.Tx) any {
			s.findDocument(c, r, fileKey, "write", false)
			copyStorageFile(file.Path, filepath.Join(staticDir, hash), true)
			execSQL(c, "INSERT INTO files (file_key,hash,name,mime,size,creator_id,privileges) VALUES (?,?,?,?,?,?,JSON_OBJECT('mode','inherit'))", fileKey, hash, name, mimeType, file.Size, user["id"])
			return nil
		})
		jsonResponse(w, 200, Row{"hash": hash, "creator": creator(user)})
	})
	s.route("GET /api/files/{fileKey}", "required", func(w http.ResponseWriter, r *http.Request) {
		files := s.tx(false, func(c *sql.Tx) any {
			fileKey := keyValue(r.PathValue("fileKey"), "文档标识")
			s.findDocument(c, r, fileKey, "read", false)
			rows := query(c, "SELECT f.hash,f.name,f.mime,f.creator_id,u.name AS creator_name,u.avator AS creator_avator FROM files f LEFT JOIN users u ON u.id=f.creator_id WHERE f.file_key=?", fileKey)
			result := make([]Row, 0, len(rows))
			for _, file := range rows {
				result = append(result, Row{"hash": file["hash"], "name": file["name"], "mime": file["mime"], "privileges": Row{"mode": "inherit"}, "creator": Row{"id": file["creator_id"], "name": file["creator_name"], "avator": file["creator_avator"]}})
			}
			return result
		})
		jsonResponse(w, 200, files)
	})
	s.route("GET /api/download/{fileKey}/{hash}", "required", func(w http.ResponseWriter, r *http.Request) {
		file := s.tx(false, func(c *sql.Tx) any { _, _, file := s.attachment(c, r); return file }).(Row)
		storageFile(w, r, filepath.Join(staticDir, str(file["hash"])), str(file["name"]), "application/octet-stream", false, "private, no-store", "附件文件不存在")
	})
	s.route("GET /api/upload-progress/{fileKey}/{hash}", "required", func(w http.ResponseWriter, r *http.Request) {
		file := s.tx(false, func(c *sql.Tx) any { _, _, file := s.attachment(c, r); return file }).(Row)
		jsonResponse(w, 200, Row{"uploadId": file["hash"], "status": "COMPLETED", "totalBytes": num(file["size"]), "persistedBytes": num(file["size"]), "uploadedPercent": 100})
	})
	s.route("DELETE /api/attachment/{fileKey}/{hash}", "required", func(w http.ResponseWriter, r *http.Request) {
		s.tx(true, func(c *sql.Tx) any {
			fileKey, hash, _ := s.attachment(c, r)
			execSQL(c, "DELETE FROM files WHERE file_key=? AND hash=?", fileKey, hash)
			// Shared hashes remain on disk for concurrent uploads; orphan cleanup is separate.
			return nil
		})
		jsonResponse(w, 200, Row{"success": true})
	})
}
