package updater

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Commands never interpolate manifest/input values into a shell. Database shell
// snippets below are fixed and expand credentials only inside the DB container.
func (s *Server) command(ctx context.Context, input io.Reader, output io.Writer, args ...string) error {
	command := exec.CommandContext(ctx, "docker", args...)
	command.Dir = s.deploy
	command.Stdin = input
	command.Stdout = output
	command.Stderr = io.Discard
	return command.Run()
}

func (s *Server) compose(ctx context.Context, envFile string, input io.Reader, output io.Writer, args ...string) error {
	prefix := []string{"compose", "--project-directory", s.deploy, "--env-file", envFile, "-f", filepath.Join(s.deploy, "compose.yaml")}
	return s.command(ctx, input, output, append(prefix, args...)...)
}

func (s *Server) loadImages(parent context.Context, directory string, release Release) error {
	ctx, cancel := context.WithTimeout(parent, 15*time.Minute)
	defer cancel()
	if err := s.command(ctx, nil, io.Discard, "load", "--input", filepath.Join(directory, "images.tar")); err != nil {
		return errors.New("Docker 镜像载入失败")
	}
	return s.verifyImages(ctx, release)
}
func (s *Server) verifyImages(parent context.Context, release Release) error {
	ctx, cancel := context.WithTimeout(parent, time.Minute)
	defer cancel()
	for _, image := range []struct{ tag, id string }{{release.APIImage, release.APIImageID}} {
		var output bytes.Buffer
		if err := s.command(ctx, nil, &output, "image", "inspect", "--format", "{{.Id}}", image.tag); err != nil {
			return errors.New("发行镜像不存在")
		}
		if err := imageInspectionMismatch(image.id, output.String()); err != nil {
			return err
		}
	}
	return nil
}

func parseEnv(raw []byte) (map[string]string, error) {
	values := make(map[string]string)
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, errors.New("运行环境文件格式不合法")
		}
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if strings.HasPrefix(value, "\"") {
			decoded, err := strconv.Unquote(value)
			if err != nil {
				return nil, errors.New("运行环境文件格式不合法")
			}
			value = decoded
		} else if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") && len(value) >= 2 {
			value = value[1 : len(value)-1]
		}
		values[name] = value
	}
	return values, nil
}

func nextEnvironment(raw []byte, release Release) ([]byte, error) {
	values, err := parseEnv(raw)
	if err != nil {
		return nil, err
	}
	if !versionPattern.MatchString(values["VOEX_VERSION"]) || values["COMPOSE_PROJECT_NAME"] == "" {
		return nil, errors.New("缺少当前部署版本或项目名称")
	}
	updates := map[string]string{
		"VOEX_API_IMAGE": release.APIImageID,
		"VOEX_VERSION":   release.Version, "VOEX_SCHEMA_VERSION": release.SchemaVersion,
	}
	lines := strings.Split(string(raw), "\n")
	seen := make(map[string]bool)
	for i, line := range lines {
		name, _, ok := strings.Cut(strings.TrimSpace(line), "=")
		name = strings.TrimSpace(name)
		if value, update := updates[name]; ok && update {
			if seen[name] {
				return nil, errors.New("运行环境包含重复版本字段")
			}
			lines[i], seen[name] = name+"="+value, true
		}
	}
	for _, name := range []string{"VOEX_API_IMAGE", "VOEX_VERSION", "VOEX_SCHEMA_VERSION"} {
		if !seen[name] {
			lines = append(lines, name+"="+updates[name])
		}
	}
	return []byte(strings.Join(lines, "\n")), nil
}

func (s *Server) schemaPreflight(envFile string, release Release) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var output bytes.Buffer
	if err := s.compose(ctx, envFile, nil, &output, "run", "--rm", "--no-deps", "api", "schema"); err != nil {
		return errors.New("新版数据库迁移预检未通过")
	}
	var result struct {
		CanMigrate bool   `json:"can_migrate"`
		Target     string `json:"target"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || !result.CanMigrate || result.Target != release.SchemaVersion {
		return errors.New("新版数据库不支持当前迁移路径")
	}
	return nil
}

func (s *Server) backupDatabase(directory string) error {
	file, err := os.OpenFile(filepath.Join(directory, "db.sql"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("无法创建数据库备份")
	}
	defer file.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	const dump = `exec mysqldump -uroot -p"$MYSQL_ROOT_PASSWORD" --single-transaction --routines --triggers --events --hex-blob --add-drop-database --databases "$MYSQL_DATABASE"`
	if err := s.compose(ctx, s.runtime(), nil, file, "exec", "-T", "db", "sh", "-c", dump); err != nil {
		return errors.New("数据库备份未完成")
	}
	info, err := file.Stat()
	if err != nil || info.Size() == 0 {
		return errors.New("数据库备份为空")
	}
	if err := file.Sync(); err != nil {
		return errors.New("数据库备份无法持久化")
	}
	return nil
}

func (s *Server) restoreDatabase(directory string) error {
	file, err := os.Open(filepath.Join(directory, "db.sql"))
	if err != nil {
		return errors.New("数据库备份不可用")
	}
	defer file.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	const restore = `exec mysql -uroot -p"$MYSQL_ROOT_PASSWORD"`
	if err := s.compose(ctx, s.runtime(), file, io.Discard, "exec", "-T", "db", "sh", "-c", restore); err != nil {
		return errors.New("数据库恢复失败")
	}
	return nil
}

func staticSize(root string) (uint64, error) {
	var total uint64
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && entry.Name() == ".uploads" {
			return filepath.SkipDir
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return errors.New("静态存储包含非普通文件")
		}
		if info.Mode().IsRegular() {
			total += uint64(info.Size()) + 1024
		}
		return nil
	})
	return total, err
}

// Migrations may only change MySQL. Immutable static files are snapshotted for
// manual disaster recovery; database rollback must never delete shared objects.
func (s *Server) backupStatic(directory string) error {
	file, err := os.OpenFile(filepath.Join(directory, "static.tar"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("无法创建静态文件备份")
	}
	defer file.Close()
	archive := tar.NewWriter(file)
	err = filepath.WalkDir(s.static, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && entry.Name() == ".uploads" {
			return filepath.SkipDir
		}
		relative, err := filepath.Rel(s.static, name)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return errors.New("静态存储包含非普通文件")
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relative)
		if err := archive.WriteHeader(header); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		source, err := os.Open(name)
		if err != nil {
			return err
		}
		_, copyErr := io.CopyN(archive, source, info.Size())
		closeErr := source.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	closeErr := archive.Close()
	if err != nil || closeErr != nil {
		return errors.New("静态文件备份未完成")
	}
	if err := file.Sync(); err != nil {
		return errors.New("静态文件备份无法持久化")
	}
	return nil
}

func (s *Server) waitHealthy(version string, schema string) error {
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		if s.apiHealthy(version, schema) && s.webHealthy(version) {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return errors.New("目标版本健康检查未通过")
}
func (s *Server) fetchJSON(address string, value any) bool {
	response, err := s.client.Get(address)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false
	}
	return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(value) == nil
}
func (s *Server) apiHealthy(version string, schema string) bool {
	var result struct {
		Version       string `json:"version"`
		SchemaVersion string `json:"schemaVersion"`
	}
	return s.fetchJSON("http://api:8090/api/health/ready", &result) && result.Version == version && result.SchemaVersion == schema
}
func (s *Server) webHealthy(version string) bool {
	response, err := s.client.Get("http://web/app/")
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false
	}
	var result struct {
		Version string `json:"version"`
	}
	return s.fetchJSON("http://web/app/version.json", &result) && result.Version == version
}

// This name is generated from the durable job ID, never from uploaded input.
func (s *Server) removeMigration(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var output bytes.Buffer
	if err := s.command(ctx, nil, &output, "container", "ls", "--all", "--filter", "name=^/"+name+"$", "--format", "{{.ID}}"); err != nil {
		return errors.New("无法确认迁移容器已停止，数据库恢复被阻止")
	}
	if strings.TrimSpace(output.String()) == "" {
		return nil
	}
	if err := s.command(ctx, nil, io.Discard, "container", "rm", "--force", name); err != nil {
		return errors.New("迁移容器未完全停止，数据库恢复被阻止")
	}
	return nil
}
