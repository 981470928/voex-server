package updater

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (s *Server) startUpgrade(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Version   string `json:"version"`
		RequestID string `json:"requestId"`
	}
	if err := readJSON(r, &input); err != nil || !versionPattern.MatchString(input.Version) || !requestPattern.MatchString(input.RequestID) {
		failHTTP(w, 400, "升级版本或请求标识不合法")
		return
	}
	s.mu.Lock()
	if id := s.requests[input.RequestID]; id != "" {
		previous := s.jobs[id]
		job, token := previous.Job, previous.WatchToken
		s.mu.Unlock()
		if job.Version != input.Version {
			failHTTP(w, 409, "请求标识已经用于其它版本")
			return
		}
		writeJSON(w, 202, map[string]any{"job": job, "watchToken": token})
		return
	}
	if s.busy {
		s.mu.Unlock()
		failHTTP(w, 409, "已有导入或升级正在进行")
		return
	}
	if _, err := os.Lstat(s.maintenance()); !errors.Is(err, os.ErrNotExist) {
		s.mu.Unlock()
		failHTTP(w, 409, "部署处于维护状态，需要先完成恢复")
		return
	}
	release, ok := s.releases[input.Version]
	if !ok {
		s.mu.Unlock()
		failHTTP(w, 404, "发行版本尚未导入")
		return
	}
	raw, err := os.ReadFile(s.runtime())
	values, parseErr := parseEnv(raw)
	if err != nil || parseErr != nil || values["VOEX_VERSION"] == "" {
		s.mu.Unlock()
		failHTTP(w, 500, "无法读取当前部署版本")
		return
	}
	allowed := false
	for _, version := range release.FromVersions {
		if version == values["VOEX_VERSION"] {
			allowed = true
		}
	}
	if !allowed || release.Version == values["VOEX_VERSION"] {
		s.mu.Unlock()
		failHTTP(w, 409, "发行包不支持从当前版本升级")
		return
	}
	id, idErr := randomToken(16)
	token, tokenErr := randomToken(32)
	if idErr != nil || tokenErr != nil {
		s.mu.Unlock()
		failHTTP(w, 500, "无法创建升级任务")
		return
	}
	now := time.Now().UTC()
	record := &storedJob{Job: Job{ID: id, RequestID: input.RequestID, Version: release.Version, FromVersion: values["VOEX_VERSION"], Status: "queued", Step: "queued", Message: "升级任务已排队", CreatedAt: now, UpdatedAt: now}, WatchToken: token}
	if err := s.saveJobLocked(record); err != nil {
		s.mu.Unlock()
		failHTTP(w, 500, "升级任务无法持久化")
		return
	}
	s.jobs[id], s.requests[input.RequestID], s.current, s.busy = record, id, id, true
	job := record.Job
	s.mu.Unlock()
	go s.runUpgrade(id, release)
	writeJSON(w, 202, map[string]any{"job": job, "watchToken": token})
}

func (s *Server) step(id, step, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.jobs[id]
	now := time.Now().UTC()
	record.Job.Status, record.Job.Step, record.Job.Message, record.Job.UpdatedAt = "running", step, message, now
	if record.Job.StartedAt == nil {
		record.Job.StartedAt = &now
	}
	if err := s.saveJobLocked(record); err != nil {
		return errors.New("任务进度无法持久化")
	}
	return nil
}
func (s *Server) finish(id, status, step, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.jobs[id]
	now := time.Now().UTC()
	record.Job.Status, record.Job.Step, record.Job.Message = status, step, message
	record.Job.UpdatedAt, record.Job.FinishedAt = now, &now
	if err := s.saveJobLocked(record); err != nil {
		return errors.New("任务结果无法持久化")
	}
	return nil
}

// Serialize completion with status readers: persist the terminal candidate first,
// remove maintenance, and only then expose success in memory. If the marker
// cannot be removed, readers keep seeing running until rollback records failure.
func (s *Server) complete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.jobs[id]
	completed := *record
	now := time.Now().UTC()
	completed.Job.Status, completed.Job.Step = "succeeded", "completed"
	completed.Job.Message = "升级完成，版本和数据库健康检查通过"
	completed.Job.UpdatedAt, completed.Job.FinishedAt = now, &now
	if err := s.saveJobLocked(&completed); err != nil {
		return errors.New("任务结果无法持久化")
	}
	if err := os.Remove(s.maintenance()); err != nil {
		// A failed rewrite is also reconciled at startup using the retained marker.
		_ = s.saveJobLocked(record)
		return errors.New("服务已就绪，但无法解除维护模式")
	}
	*record = completed
	return nil
}

type recovery struct {
	marker, databaseChanged bool
	directory               string
	original                []byte
	version                 string
	schema                  string
	migrationContainer      string
	frontendVersion         string
}

func (s *Server) runUpgrade(id string, release Release) {
	state := &recovery{directory: filepath.Join(s.data, "backups", id)}
	defer func() {
		// This guard also contains an unexpected failure in rollback itself.
		if recover() != nil {
			log.Print("upgrade worker panic contained; manual recovery required")
			_ = atomicWriteMode(s.maintenance(), []byte(id+"\n"), 0644)
			_ = s.finish(id, "interrupted", "recovery_required", "升级处理异常中断，维护模式保留，需要人工恢复")
		}
		s.mu.Lock()
		s.busy = false
		s.mu.Unlock()
	}()
	err := func() (failure error) {
		defer func() {
			if recover() != nil {
				failure = errors.New("升级处理出现异常")
			}
		}()
		return s.performUpgrade(id, release, state)
	}()
	if err == nil {
		return
	}
	message, step := err.Error(), "failed"
	if state.marker {
		// Ensure rollback never starts with traffic accidentally reopened by an
		// interrupted completion transition.
		markerErr := atomicWriteMode(s.maintenance(), []byte(id+"\n"), 0644)
		recoveryErr := markerErr
		if markerErr == nil {
			recoveryErr = s.rollback(id, state)
		}
		if recoveryErr != nil {
			message += "；自动恢复未完成，保持维护模式，需要人工恢复"
			step = "recovery_required"
		} else if state.databaseChanged {
			message += "；数据库备份和原版本已恢复"
		} else {
			message += "；原版本已恢复，数据库未发生升级变更"
		}
	} else {
		message += "；当前版本未替换"
	}
	if err := s.finish(id, "failed", step, message); err != nil {
		log.Print("cannot persist failed upgrade result")
	}
}

func (s *Server) performUpgrade(id string, release Release, state *recovery) error {
	if err := s.step(id, "preflight", "正在验证版本、镜像、迁移兼容性和磁盘空间"); err != nil {
		return err
	}
	original, err := os.ReadFile(s.runtime())
	if err != nil {
		return errors.New("当前运行环境不可用")
	}
	values, err := parseEnv(original)
	if err != nil {
		return err
	}
	schema := values["VOEX_SCHEMA_VERSION"]
	if !schemaPattern.MatchString(schema) {
		return errors.New("当前数据库版本无效")
	}
	state.original, state.version, state.schema = original, values["VOEX_VERSION"], schema
	allowed := false
	for _, version := range release.FromVersions {
		if version == state.version {
			allowed = true
		}
	}
	if !allowed || state.version == release.Version {
		return errors.New("发行包不支持当前版本")
	}
	directory := filepath.Join(s.data, "releases", release.Version)
	verified, err := s.readRelease(directory)
	if err != nil || verified.APIImageID != release.APIImageID || verified.FrontendSHA256 != release.FrontendSHA256 || verified.ImagesSHA256 != release.ImagesSHA256 {
		return errors.New("已导入发行包验证失败")
	}
	digest, err := hashFile(filepath.Join(directory, "images.tar"))
	if err != nil || digest != release.ImagesSHA256 {
		return errors.New("已导入镜像归档摘要不一致")
	}
	frontendDigest, err := hashFile(filepath.Join(directory, "frontend.tar.gz"))
	if err != nil || frontendDigest != release.FrontendSHA256 {
		return errors.New("已导入前端归档摘要不一致")
	}
	if err := PrepareFrontend(filepath.Join(directory, "frontend.tar.gz"), filepath.Join(s.deploy, "frontend"), release.Version); err != nil {
		return err
	}
	state.frontendVersion, err = CurrentFrontend(filepath.Join(s.deploy, "frontend"))
	if err != nil {
		return err
	}
	if state.frontendVersion != state.version {
		return errors.New("当前 API 与前端版本不一致，需要先修复部署")
	}
	if err := s.verifyImages(context.Background(), release); err != nil {
		return err
	}
	if err := privateDir(state.directory); err != nil {
		return errors.New("无法创建升级备份目录")
	}
	next, err := nextEnvironment(original, release)
	if err != nil {
		return err
	}
	nextFile := filepath.Join(state.directory, "next.env")
	if err := atomicWrite(nextFile, next); err != nil {
		return errors.New("无法保存目标运行环境")
	}
	if err := s.schemaPreflight(nextFile, release); err != nil {
		return err
	}
	staticBytes, err := staticSize(s.static)
	if err != nil {
		return errors.New("无法预估静态存储备份")
	}
	databaseBytes, err := s.databaseSize()
	if err != nil {
		return err
	}
	images, err := os.Stat(filepath.Join(directory, "images.tar"))
	if err != nil {
		return errors.New("发行归档不可用")
	}
	free, err := availableBytes(state.directory)
	// SQL hex blobs can double raw data size; retain index and Docker image headroom.
	needed := staticBytes + databaseBytes*3 + uint64(images.Size())*2 + (1 << 30)
	if err != nil || free < needed {
		return errors.New("磁盘可用空间不足以保留数据库、静态备份和镜像余量")
	}
	if err := atomicWrite(filepath.Join(state.directory, "frontend-version"), []byte(state.frontendVersion+"\n")); err != nil {
		return errors.New("原前端版本备份失败")
	}
	s.mu.Lock()
	s.jobs[id].Job.PreviousFrontendVersion = state.frontendVersion
	err = s.saveJobLocked(s.jobs[id])
	s.mu.Unlock()
	if err != nil {
		return errors.New("原前端版本状态无法持久化")
	}
	if err := atomicWrite(filepath.Join(state.directory, "runtime.env"), original); err != nil {
		return errors.New("原版本运行环境备份失败")
	}
	state.marker = true
	if err := atomicWriteMode(s.maintenance(), []byte(id+"\n"), 0644); err != nil {
		return errors.New("无法启用维护模式")
	}
	if err := s.step(id, "stopping", "已进入维护模式，正在停止 API 业务和后台任务"); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	err = s.compose(ctx, s.runtime(), nil, io.Discard, "stop", "-t", "40", "api")
	cancel()
	if err != nil {
		return errors.New("无法完全停止旧版 API")
	}
	if err := s.step(id, "backup_database", "正在备份数据库"); err != nil {
		return err
	}
	if err := s.backupDatabase(state.directory); err != nil {
		return err
	}
	if err := s.step(id, "backup_static", "正在归档不可变静态文件"); err != nil {
		return err
	}
	if err := s.backupStatic(state.directory); err != nil {
		return err
	}
	s.mu.Lock()
	s.jobs[id].Job.Backup = filepath.ToSlash(filepath.Join("backups", id))
	err = s.saveJobLocked(s.jobs[id])
	s.mu.Unlock()
	if err != nil {
		return errors.New("备份状态无法持久化")
	}
	if err := s.step(id, "migrating", "备份已完成，正在执行新版数据库迁移"); err != nil {
		return err
	}
	if err := atomicWrite(s.runtime(), next); err != nil {
		return errors.New("无法切换目标运行环境")
	}
	// A failed migration may have committed DDL. Always restore the actual SQL
	// snapshot before returning to the old image, never merely switch image tags.
	state.databaseChanged = true
	state.migrationContainer = "voex-migrate-" + id
	ctx, cancel = context.WithTimeout(context.Background(), 15*time.Minute)
	err = s.compose(ctx, s.runtime(), nil, io.Discard, "run", "--rm", "--no-deps", "--name", state.migrationContainer, "api", "migrate")
	cancel()
	if err != nil {
		return errors.New("数据库迁移失败")
	}
	if err := s.step(id, "starting", "正在启动新版 API"); err != nil {
		return err
	}
	ctx, cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	err = s.compose(ctx, s.runtime(), nil, io.Discard, "up", "-d", "--no-deps", "api")
	cancel()
	if err != nil {
		return errors.New("新版服务启动失败")
	}
	if err := s.step(id, "switch_frontend", "正在原子切换前端 dist 版本"); err != nil {
		return err
	}
	if err := SwitchFrontend(filepath.Join(s.deploy, "frontend"), release.Version); err != nil {
		return err
	}
	if err := s.step(id, "healthcheck", "正在验证 API、数据库版本与前端版本"); err != nil {
		return err
	}
	if err := s.waitHealthy(release.Version, release.SchemaVersion); err != nil {
		return err
	}
	if err := s.complete(id); err != nil {
		return err
	}
	state.marker = false
	return nil
}

func (s *Server) rollback(id string, state *recovery) error {
	_ = s.step(id, "rolling_back", "升级未完成，正在恢复数据库和原版本")
	// Restore the old frontend entry first; the fixed Nginx keeps serving
	// status polling and retained hashed assets throughout database recovery.
	if state.frontendVersion != "" {
		if err := SwitchFrontend(filepath.Join(s.deploy, "frontend"), state.frontendVersion); err != nil {
			return errors.New("原前端版本恢复失败")
		}
	}
	// Killing the compose CLI on timeout does not guarantee its migration
	// container stopped. Explicitly quiesce it before restoring the SQL snapshot.
	if state.migrationContainer != "" {
		if err := s.removeMigration(state.migrationContainer); err != nil {
			return err
		}
	}
	// Never restore SQL while either old or new API processes may still write.
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	err := s.compose(ctx, s.runtime(), nil, io.Discard, "stop", "-t", "40", "api")
	cancel()
	if err != nil {
		return errors.New("无法停止 API，数据库恢复被阻止")
	}
	if state.databaseChanged {
		if err := s.restoreDatabase(state.directory); err != nil {
			return err
		}
	}
	if err := atomicWrite(s.runtime(), state.original); err != nil {
		return errors.New("原版本运行环境恢复失败")
	}
	ctx, cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	err = s.compose(ctx, s.runtime(), nil, io.Discard, "up", "-d", "--no-deps", "api")
	cancel()
	if err != nil {
		return errors.New("原版本服务恢复失败")
	}
	if err := s.waitHealthy(state.version, state.schema); err != nil {
		return errors.New("原版本恢复健康检查失败")
	}
	if err := os.Remove(s.maintenance()); err != nil {
		return errors.New("恢复后无法解除维护模式")
	}
	return nil
}

func (s *Server) databaseSize() (uint64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var output bytes.Buffer
	const query = `exec mysql -uroot -p"$MYSQL_ROOT_PASSWORD" --batch --skip-column-names "$MYSQL_DATABASE" -e 'SELECT COALESCE(SUM(data_length+index_length),0) FROM information_schema.tables WHERE table_schema=DATABASE()'`
	if err := s.compose(ctx, s.runtime(), nil, &output, "exec", "-T", "db", "sh", "-c", query); err != nil {
		return 0, errors.New("数据库备份空间预检失败")
	}
	size, err := strconv.ParseUint(strings.TrimSpace(output.String()), 10, 64)
	if err != nil {
		return 0, errors.New("数据库空间预检返回无效结果")
	}
	return size, nil
}
