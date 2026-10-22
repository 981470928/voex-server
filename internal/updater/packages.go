package updater

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

const maximumPackage int64 = 2 << 30
const maximumImages int64 = 8 << 30

var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9][A-Za-z0-9.-]{0,63})?$`)
var imagePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:/-]*:[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var imageIDPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var schemaPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var requestPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func (s *Server) readPublicKey() (ed25519.PublicKey, error) {
	value, err := os.ReadFile(s.publicKey)
	if err != nil {
		return nil, errors.New("release public key is unavailable")
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(value)))
	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil, errors.New("release public key must be base64 raw Ed25519")
	}
	return ed25519.PublicKey(key), nil
}

func (s *Server) readRelease(directory string) (Release, error) {
	var release Release
	raw, err := os.ReadFile(filepath.Join(directory, "release.json"))
	if err != nil || len(raw) > 128<<10 {
		return release, errors.New("发行清单不合法")
	}
	encoded, err := os.ReadFile(filepath.Join(directory, "release.sig"))
	if err != nil || len(encoded) > 1024 {
		return release, errors.New("发行签名不合法")
	}
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil || len(signature) != ed25519.SignatureSize {
		return release, errors.New("发行签名不合法")
	}
	key, err := s.readPublicKey()
	if err != nil {
		return release, errors.New("发行公钥不可用")
	}
	if !ed25519.Verify(key, raw, signature) {
		return release, errors.New("发行签名验证失败")
	}
	var format struct {
		FormatVersion int `json:"formatVersion"`
	}
	if err := json.Unmarshal(raw, &format); err != nil {
		return release, errors.New("发行清单格式不合法")
	}
	if format.FormatVersion != 2 {
		return release, errors.New("仅支持 formatVersion=2 的 API 镜像加前端 dist 升级包，v1 镜像包不兼容")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&release); err != nil || decoder.Decode(new(any)) != io.EOF {
		return release, errors.New("发行清单格式不合法")
	}
	if err := validateRelease(release); err != nil {
		return release, err
	}
	return release, nil
}

func validateRelease(release Release) error {
	if release.FormatVersion != 2 {
		return errors.New("仅支持 formatVersion=2 升级包")
	}
	if !versionPattern.MatchString(release.Version) || !schemaPattern.MatchString(release.SchemaVersion) || release.CreatedAt.IsZero() || len(release.Notes) > 32<<10 || len(release.FromVersions) == 0 || len(release.FromVersions) > 256 {
		return errors.New("发行版本或迁移范围不合法")
	}
	for _, version := range release.FromVersions {
		if !versionPattern.MatchString(version) {
			return errors.New("发行迁移范围不合法")
		}
	}
	for _, image := range []string{release.APIImage} {
		if !imagePattern.MatchString(image) || strings.Contains(image, "@") || !strings.HasSuffix(image, ":"+release.Version) {
			return errors.New("发行镜像必须使用与版本一致的固定 tag")
		}
	}
	if !hashPattern.MatchString(release.ImagesSHA256) || !imageIDPattern.MatchString(release.APIImageID) || !hashPattern.MatchString(release.FrontendSHA256) {
		return errors.New("发行内容摘要不合法")
	}
	return nil
}

func availableBytes(name string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(name, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}

func hashFile(name string) (string, error) {
	file, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (s *Server) importPackage(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		failHTTP(w, 409, "已有导入或升级正在进行")
		return
	}
	s.busy = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.busy = false; s.mu.Unlock() }()
	r.Body = http.MaxBytesReader(w, r.Body, maximumPackage+(1<<20))
	reader, err := r.MultipartReader()
	if err != nil {
		failHTTP(w, 400, "请使用 multipart 上传升级包")
		return
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "file" || part.FileName() == "" {
		failHTTP(w, 400, "升级包字段必须为 file")
		return
	}
	stage, err := os.MkdirTemp(filepath.Join(s.data, "imports"), "release-")
	if err != nil {
		failHTTP(w, 500, "无法创建升级包暂存目录")
		return
	}
	defer os.RemoveAll(stage)
	// The limit counts compressed file bytes, separately from multipart overhead.
	compressed := &io.LimitedReader{R: part, N: maximumPackage + 1}
	unpackErr := unpackPackage(stage, compressed)
	if compressed.N <= 0 {
		failHTTP(w, 413, "升级包超过 2 GiB 限制")
		return
	}
	if unpackErr != nil {
		failHTTP(w, 400, unpackErr.Error())
		return
	}
	if _, err := reader.NextPart(); err != io.EOF {
		failHTTP(w, 400, "升级包只能包含一个 file 字段")
		return
	}
	release, err := s.readRelease(stage)
	if err != nil {
		failHTTP(w, 400, err.Error())
		return
	}
	s.mu.Lock()
	_, exists := s.releases[release.Version]
	s.mu.Unlock()
	if exists {
		failHTTP(w, 409, "该版本已经导入")
		return
	}
	runtime, runtimeErr := os.ReadFile(s.runtime())
	current, parseErr := parseEnv(runtime)
	if runtimeErr != nil || parseErr != nil || current["VOEX_VERSION"] == "" {
		failHTTP(w, 500, "无法核对当前部署版本")
		return
	}
	if current["VOEX_VERSION"] == release.Version {
		failHTTP(w, 409, "不能载入并覆盖当前版本的镜像 tag")
		return
	}
	digest, err := hashFile(filepath.Join(stage, "images.tar"))
	if err != nil || digest != release.ImagesSHA256 {
		failHTTP(w, 400, "镜像归档摘要验证失败")
		return
	}
	frontendDigest, err := hashFile(filepath.Join(stage, "frontend.tar.gz"))
	if err != nil || frontendDigest != release.FrontendSHA256 {
		failHTTP(w, 400, "前端归档摘要验证失败")
		return
	}
	if err := validateAPIArchive(filepath.Join(stage, "images.tar"), release); err != nil {
		failHTTP(w, 400, err.Error())
		return
	}
	if err := s.loadImages(r.Context(), stage, release); err != nil {
		failHTTP(w, 400, err.Error())
		return
	}
	if err := PrepareFrontend(filepath.Join(stage, "frontend.tar.gz"), filepath.Join(s.deploy, "frontend"), release.Version); err != nil {
		failHTTP(w, 400, err.Error())
		return
	}
	target := filepath.Join(s.data, "releases", release.Version)
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		failHTTP(w, 409, "发行目录已经存在")
		return
	}
	if err := os.Rename(stage, target); err != nil {
		failHTTP(w, 500, "无法保存发行包")
		return
	}
	s.mu.Lock()
	s.releases[release.Version] = release
	s.mu.Unlock()
	writeJSON(w, 201, map[string]any{"release": release})
}

func unpackPackage(directory string, compressed *io.LimitedReader) error {
	buffered := bufio.NewReader(compressed)
	magic, err := buffered.Peek(2)
	if err != nil {
		return errors.New("升级包为空或已损坏")
	}
	if magic[0] != 0x1f || magic[1] != 0x8b {
		return errors.New("v2 升级包必须为 tar.gz")
	}
	gz, err := gzip.NewReader(buffered)
	if err != nil {
		return errors.New("升级包 gzip 数据损坏")
	}
	defer gz.Close()
	var source io.Reader = gz
	archive := tar.NewReader(source)
	seen := make(map[string]bool)
	var total int64
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.New("升级包归档损坏")
		}
		var limit int64
		switch header.Name {
		case "release.json":
			limit = 128 << 10
		case "release.sig":
			limit = 1024
		case "images.tar":
			limit = maximumImages
		case "frontend.tar.gz":
			limit = maximumFrontend
		default:
			return errors.New("升级包包含未允许的文件或路径")
		}
		if header.Typeflag != tar.TypeReg || seen[header.Name] || header.Size < 1 || header.Size > limit {
			return errors.New("升级包条目类型、数量或大小不合法")
		}
		seen[header.Name] = true
		total += header.Size
		if total > maximumImages+maximumFrontend+(129<<10) {
			return errors.New("升级包展开内容过大")
		}
		free, err := availableBytes(directory)
		if err != nil || free < uint64(header.Size)*2+(256<<20) {
			return errors.New("磁盘空间不足以展开和载入升级包")
		}
		file, err := os.OpenFile(filepath.Join(directory, header.Name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return errors.New("无法写入升级包")
		}
		copied, copyErr := io.CopyN(file, archive, header.Size)
		syncErr := file.Sync()
		closeErr := file.Close()
		if copyErr != nil || copied != header.Size || syncErr != nil || closeErr != nil {
			return errors.New("升级包写入未完成")
		}
	}
	if len(seen) != 4 {
		return errors.New("仅支持 v2 升级包：必须且只能包含 release.json、release.sig、images.tar 和 frontend.tar.gz")
	}
	// Force gzip checksum verification and reject hidden nonzero trailing payloads.
	tail, err := io.ReadAll(io.LimitReader(source, 1<<20))
	if err != nil || len(tail) >= 1<<20 {
		return errors.New("升级包尾部不合法")
	}
	for _, value := range tail {
		if value != 0 {
			return errors.New("升级包包含额外数据")
		}
	}
	if compressed.N <= 0 {
		return errors.New("升级包超过 2 GiB 限制")
	}
	return nil
}

func imageInspectionMismatch(expected, actual string) error {
	if strings.TrimSpace(actual) != expected {
		return fmt.Errorf("镜像 ID 与签名清单不一致")
	}
	return nil
}

// Docker save archives must carry exactly the one versioned API image declared
// by the signed release; no Nginx image is loaded as part of an application update.
func validateAPIArchive(filename string, release Release) error {
	file, err := os.Open(filename)
	if err != nil {
		return errors.New("API 镜像归档不可用")
	}
	defer file.Close()
	archive := tar.NewReader(file)
	found := false
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.New("API 镜像归档损坏")
		}
		if header.Name != "manifest.json" {
			continue
		}
		if found || header.Typeflag != tar.TypeReg || header.Size < 1 || header.Size > 256<<10 {
			return errors.New("API 镜像清单不合法")
		}
		found = true
		data, err := io.ReadAll(io.LimitReader(archive, (256<<10)+1))
		var images []struct {
			RepoTags []string `json:"RepoTags"`
		}
		if err != nil || json.Unmarshal(data, &images) != nil || len(images) != 1 || len(images[0].RepoTags) != 1 || images[0].RepoTags[0] != release.APIImage {
			return errors.New("images.tar 必须且只能包含清单声明的一个 API 镜像 tag")
		}
	}
	if !found {
		return errors.New("API 镜像归档缺少 Docker manifest.json")
	}
	return nil
}
