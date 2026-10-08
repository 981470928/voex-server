package filestore

import (
	"context"
	"crypto/sha256"
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
	"voex-server/internal/shared"
)

var directoryPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}(/[A-Za-z0-9_-]{1,64})*$`)
var attachmentHashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var assetStoragePattern = regexp.MustCompile(`^[a-f0-9-]{36}(\.webp)?$`)

// Store owns local asset and content-addressed attachment directories.
type Store struct{ staticDir, uploadDir, tempDir string }

func New(staticDir, uploadDir string) *Store {
	store := &Store{staticDir: filepath.Clean(staticDir), uploadDir: filepath.Clean(uploadDir)}
	store.tempDir = filepath.Join(store.uploadDir, ".incoming")
	for _, directory := range []string{store.staticDir, store.uploadDir, store.tempDir} {
		if !filepath.IsAbs(directory) {
			panic("Storage directory must be absolute")
		}
		shared.Must(os.MkdirAll(directory, 0700))
		info, err := os.Lstat(directory)
		shared.Must(err)
		resolved, err := filepath.EvalSymlinks(directory)
		shared.Must(err)
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || resolved != directory {
			panic("Storage directory must not be a symbolic link")
		}
	}
	return store
}

func SafeDirectory(value any) string {
	directory, ok := value.(string)
	if !ok || len(directory) > 255 || !directoryPattern.MatchString(directory) {
		shared.Fail(400, "path 只能包含字母、数字、短横线、下划线和分层斜杠")
	}
	return directory
}

func (s *Store) storageDirectory(directory string) string {
	SafeDirectory(directory)
	resolved := s.uploadDir
	for _, part := range strings.Split(directory, "/") {
		resolved = filepath.Join(resolved, part)
		err := os.Mkdir(resolved, 0700)
		if err != nil && !os.IsExist(err) {
			shared.Must(err)
		}
		info, err := os.Lstat(resolved)
		actual, realErr := filepath.EvalSymlinks(resolved)
		if err != nil || realErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || actual != resolved {
			shared.Fail(400, "上传目录不合法")
		}
	}
	return resolved
}

type Upload struct {
	Path   string
	Name   string
	MIME   string
	Size   int64
	Fields shared.Row
}

// Read the multipart stream with the same file, field, and size limits as the previous API.
func (s *Store) ReadUpload(w http.ResponseWriter, r *http.Request, asset bool) (upload Upload) {
	const maximum = 200 * 1024 * 1024
	upload.Fields = shared.Row{}
	complete := false
	defer func() {
		if !complete && upload.Path != "" {
			_ = os.Remove(upload.Path)
		}
	}()
	r.Body = http.MaxBytesReader(w, r.Body, maximum+1024*1024)
	reader, err := r.MultipartReader()
	if err != nil {
		shared.Fail(400, "请求格式错误，请检查上传表单")
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
			shared.Fail(400, "上传字段或文件数量不合法")
		}
		if part.FileName() == "" {
			fields++
			if fields > maxFields {
				shared.Fail(400, "上传字段或文件数量不合法")
			}
			data, err := io.ReadAll(io.LimitReader(part, maxFieldSize+1))
			if err != nil {
				uploadReadError(err)
			}
			if int64(len(data)) > maxFieldSize {
				shared.Fail(400, "上传字段或文件数量不合法")
			}
			if _, exists := upload.Fields[name]; exists {
				shared.Fail(400, "上传字段或文件数量不合法")
			}
			upload.Fields[name] = string(data)
		} else {
			if name != "file" || upload.Path != "" {
				shared.Fail(400, "上传字段或文件数量不合法")
			}
			file, err := os.CreateTemp(s.tempDir, "upload-")
			shared.Must(err)
			upload.Path, upload.Name, upload.MIME = file.Name(), part.FileName(), part.Header.Get("Content-Type")
			upload.Size, err = io.Copy(file, io.LimitReader(part, maximum+1))
			closeErr := file.Close()
			if err != nil {
				uploadReadError(err)
			}
			shared.Must(closeErr)
			if upload.Size > maximum {
				shared.Fail(413, "上传文件超过大小限制")
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
		shared.Fail(413, "上传文件超过大小限制")
	}
	var pathError *os.PathError
	if errors.As(err, &pathError) {
		shared.Must(err)
	}
	shared.Fail(400, "请求格式错误，请检查上传表单")
}

func ComputeHash(filename string) string {
	file, err := os.Open(filename)
	shared.Must(err)
	defer file.Close()
	hash := sha256.New()
	_, err = io.Copy(hash, file)
	shared.Must(err)
	return hex.EncodeToString(hash.Sum(nil))
}

func copyStorageFile(source, destination string, reuse bool) {
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if reuse && os.IsExist(err) {
		return
	}
	shared.Must(err)
	complete := false
	defer func() {
		_ = output.Close()
		if !complete {
			_ = os.Remove(destination)
		}
	}()
	input, err := os.Open(source)
	shared.Must(err)
	defer input.Close()
	_, err = io.Copy(output, input)
	shared.Must(err)
	shared.Must(output.Close())
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

func convertStorageImage(ctx context.Context, source, destination, directory string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
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

// Download carries a previously authorized file and its response policy.
type Download struct {
	Path, Name, MIME, Cache, Missing string
	Inline, SameOrigin               bool
}

// Serve supports HEAD and byte ranges without following symlinks.
func Serve(w http.ResponseWriter, r *http.Request, download Download) {
	filename, name, mimeType, inline, cache, missing := download.Path, download.Name, download.MIME, download.Inline, download.Cache, download.Missing
	if download.SameOrigin {
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	}
	// O_NOFOLLOW closes the lstat/open race and never serves a symlink target.
	file, err := os.OpenFile(filename, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		shared.Fail(404, missing)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		shared.Fail(404, missing)
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

// Remove cleans up an incoming multipart body after its request finishes.
func (u Upload) Remove() {
	if u.Path != "" {
		_ = os.Remove(u.Path)
	}
}

type StoredAsset struct {
	Path, StorageName, MIME string
	Size                    int64
}

func (a StoredAsset) Remove() {
	if a.Path != "" {
		_ = os.Remove(a.Path)
	}
}

// SaveAsset normalizes supported images and stores other assets as opaque bytes.
func (s *Store) SaveAsset(ctx context.Context, upload Upload, id, directory string) (stored StoredAsset) {
	destination := s.storageDirectory(directory)
	stored = StoredAsset{StorageName: id, MIME: "application/octet-stream", Size: upload.Size}
	complete := false
	defer func() {
		if !complete {
			stored.Remove()
		}
	}()
	if directory == "avator" || directory == "thumbnail" {
		maximum := int64(20)
		if directory == "avator" {
			maximum = 5
		}
		if upload.Size > maximum*1024*1024 {
			shared.Fail(413, fmt.Sprintf("图片不能超过 %d MB", maximum))
		}
		stored.StorageName += ".webp"
		stored.Path = filepath.Join(destination, stored.StorageName)
		if err := convertStorageImage(ctx, upload.Path, stored.Path, directory); err != nil {
			shared.Fail(400, "请选择有效的静态 PNG、JPEG 或 WebP 图片（最多 2000 万像素）")
		}
		info, err := os.Stat(stored.Path)
		shared.Must(err)
		stored.MIME, stored.Size = "image/webp", info.Size()
	} else {
		stored.Path = filepath.Join(destination, stored.StorageName)
		copyStorageFile(upload.Path, stored.Path, false)
	}
	complete = true
	return stored
}

func (s *Store) AssetPath(directory, storageName string) string {
	if !assetStoragePattern.MatchString(storageName) {
		shared.Fail(404, "文件不存在")
	}
	return filepath.Join(s.storageDirectory(directory), storageName)
}
func (s *Store) AttachmentPath(hash string) string {
	if !attachmentHashPattern.MatchString(hash) {
		shared.Fail(404, "附件不存在")
	}
	return filepath.Join(s.staticDir, hash)
}
func (s *Store) SaveAttachment(upload Upload, hash string) {
	copyStorageFile(upload.Path, s.AttachmentPath(hash), true)
}
