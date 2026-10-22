package updater

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

const maximumFrontend int64 = 2 << 30
const maximumFrontendFile int64 = 256 << 20

// PrepareFrontend publishes a validated dist archive without changing current.
// The caller must verify its release signature and frontendSHA256 before calling.
// The initial deployment CLI may use this explicitly for a trusted local build.
func PrepareFrontend(archivePath, frontendRoot, version string) error {
	if !versionPattern.MatchString(version) || !filepath.IsAbs(frontendRoot) || filepath.Clean(frontendRoot) == "/" {
		return errors.New("前端版本或目录不合法")
	}
	lock, err := lockFrontend(frontendRoot)
	if err != nil {
		return err
	}
	defer lock.Close()
	stage, err := os.MkdirTemp(frontendRoot, ".prepare-")
	if err != nil {
		return errors.New("无法创建前端暂存目录")
	}
	defer os.RemoveAll(stage)
	app := filepath.Join(stage, "app")
	if err := os.Mkdir(app, 0755); err != nil {
		return errors.New("无法创建前端应用目录")
	}
	if err := extractFrontend(archivePath, app); err != nil {
		return err
	}
	if err := validateFrontend(app, version); err != nil {
		return err
	}
	target := filepath.Join(frontendRoot, "releases", version)
	if err := realDirectories(filepath.Join(frontendRoot, "releases")); err != nil {
		return err
	}
	exists := false
	if info, err := os.Lstat(target); err == nil {
		if !info.IsDir() {
			return errors.New("前端版本目录不是普通目录")
		}
		same, err := sameFrontend(app, filepath.Join(target, "app"))
		if err != nil || !same {
			return errors.New("相同前端版本已存在不同内容，拒绝覆盖")
		}
		exists = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("无法检查前端版本目录")
	}
	// All input and version checks finish before anything becomes publicly readable.
	if err := publishFrontendAssets(app, frontendRoot); err != nil {
		return err
	}
	if exists {
		return nil
	}
	if err := os.Chmod(stage, 0755); err != nil {
		return errors.New("无法设置前端目录权限")
	}
	if err := syncFrontendTree(stage); err != nil {
		return err
	}
	if err := os.Rename(stage, target); err != nil {
		return errors.New("无法原子发布前端版本")
	}
	return syncFrontendDirectory(filepath.Join(frontendRoot, "releases"))
}

// CurrentFrontend returns only a version addressed by a safe relative symlink.
func CurrentFrontend(frontendRoot string) (string, error) {
	if !filepath.IsAbs(frontendRoot) {
		return "", errors.New("前端目录必须为绝对路径")
	}
	target, err := os.Readlink(filepath.Join(frontendRoot, "current"))
	if err != nil {
		return "", errors.New("无法读取当前前端版本")
	}
	version, ok := strings.CutPrefix(target, "releases/")
	if !ok || !versionPattern.MatchString(version) || target != "releases/"+version {
		return "", errors.New("当前前端链接必须指向相对版本目录")
	}
	if err := existingRealDirectories(filepath.Join(frontendRoot, "releases", version, "app")); err != nil {
		return "", err
	}
	if err := validateFrontend(filepath.Join(frontendRoot, "releases", version, "app"), version); err != nil {
		return "", err
	}
	return version, nil
}

// SwitchFrontend replaces current atomically. The containing directory must be
// bind-mounted into Nginx, rather than binding the old symlink target itself.
func SwitchFrontend(frontendRoot, version string) error {
	if !versionPattern.MatchString(version) || !filepath.IsAbs(frontendRoot) {
		return errors.New("前端版本或目录不合法")
	}
	lock, err := lockFrontend(frontendRoot)
	if err != nil {
		return err
	}
	defer lock.Close()
	app := filepath.Join(frontendRoot, "releases", version, "app")
	if err := existingRealDirectories(app); err != nil {
		return err
	}
	if err := validateFrontend(app, version); err != nil {
		return err
	}
	current := filepath.Join(frontendRoot, "current")
	if info, err := os.Lstat(current); err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return errors.New("当前前端入口必须为符号链接")
		}
		if _, err := CurrentFrontend(frontendRoot); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("无法检查当前前端入口")
	}
	token, err := randomToken(16)
	if err != nil {
		return errors.New("无法创建前端切换标识")
	}
	pending := filepath.Join(frontendRoot, ".current-"+token)
	defer os.Remove(pending)
	if err := os.Symlink("releases/"+version, pending); err != nil {
		return errors.New("无法创建前端版本链接")
	}
	if err := os.Rename(pending, current); err != nil {
		return errors.New("无法原子切换前端版本")
	}
	return syncFrontendDirectory(frontendRoot)
}

func lockFrontend(root string) (*os.File, error) {
	if err := realDirectories(root); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(root, ".publish.lock"), os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, errors.New("无法打开前端发布锁")
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, errors.New("无法锁定前端发布目录")
	}
	return file, nil
}

func existingRealDirectories(name string) error {
	for current := filepath.Clean(name); current != filepath.Dir(current); current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() {
			return errors.New("前端目录不存在或包含符号链接")
		}
	}
	return nil
}
func realDirectories(name string) error {
	if !filepath.IsAbs(name) {
		return errors.New("前端目录必须为绝对路径")
	}
	// Inspect existing ancestors before mkdir to avoid creating through a symlink.
	var missing []string
	for current := filepath.Clean(name); current != filepath.Dir(current); current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			missing = append(missing, current)
			continue
		}
		if err != nil || !info.IsDir() {
			return errors.New("前端目录包含非目录或符号链接")
		}
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], 0755); err != nil && !errors.Is(err, os.ErrExist) {
			return errors.New("无法创建前端目录")
		}
		if err := os.Chmod(missing[i], 0755); err != nil {
			return errors.New("无法设置前端目录权限")
		}
	}
	return existingRealDirectories(name)
}

func frontendEntry(name string) (string, error) {
	if len(name) > 4096 || strings.ContainsAny(name, "\\\x00") || path.IsAbs(name) {
		return "", errors.New("前端归档路径不合法")
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return "", errors.New("前端归档禁止父目录路径")
		}
	}
	name = strings.TrimPrefix(name, "./")
	clean := path.Clean(name)
	if clean == "." {
		return ".", nil
	}
	if !fs.ValidPath(clean) || strings.Contains(clean, ":") || strings.Count(clean, "/") > 64 {
		return "", errors.New("前端归档路径不合法")
	}
	return clean, nil
}

func extractFrontend(archivePath, app string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return errors.New("前端归档不可用")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maximumFrontend {
		return errors.New("前端归档大小不合法")
	}
	gz, err := gzip.NewReader(file)
	if err != nil {
		return errors.New("前端归档必须为 tar.gz")
	}
	defer gz.Close()
	archive := tar.NewReader(gz)
	seen := make(map[string]bool)
	var total int64
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.New("前端归档损坏")
		}
		name, err := frontendEntry(header.Name)
		if err != nil {
			return err
		}
		if seen[name] || len(seen) >= 100000 || header.Linkname != "" {
			return errors.New("前端归档包含重复条目或链接")
		}
		seen[name] = true
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeDir {
			return errors.New("前端归档只允许普通文件和目录")
		}
		if name == "." {
			if header.Typeflag != tar.TypeDir || header.Size != 0 {
				return errors.New("前端归档根条目不合法")
			}
			continue
		}
		if header.Size < 0 || header.Size > maximumFrontendFile || (header.Typeflag == tar.TypeDir && header.Size != 0) {
			return errors.New("前端归档条目过大")
		}
		total += header.Size
		if total > maximumFrontend {
			return errors.New("前端归档展开内容超过 2 GiB")
		}
		if free, err := availableBytes(app); err != nil || free < uint64(header.Size)+(256<<20) {
			return errors.New("磁盘空间不足以展开前端归档")
		}
		target := filepath.Join(app, filepath.FromSlash(name))
		if header.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(target, 0755); err != nil {
				return errors.New("无法创建前端目录")
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return errors.New("无法创建前端目录")
		}
		output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			return errors.New("无法创建前端文件")
		}
		_, copyErr := io.CopyN(output, archive, header.Size)
		modeErr := output.Chmod(0644)
		syncErr := output.Sync()
		closeErr := output.Close()
		if copyErr != nil || modeErr != nil || syncErr != nil || closeErr != nil {
			return errors.New("前端文件未完整写入")
		}
	}
	tail, err := io.ReadAll(io.LimitReader(gz, 1<<20))
	if err != nil || len(tail) >= 1<<20 {
		return errors.New("前端归档校验或尾部不合法")
	}
	for _, value := range tail {
		if value != 0 {
			return errors.New("前端归档包含额外数据")
		}
	}
	return nil
}

func validateFrontend(app, version string) error {
	for _, name := range []string{"index.html", "version.json"} {
		info, err := os.Lstat(filepath.Join(app, name))
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || (name == "version.json" && info.Size() > 64<<10) {
			return errors.New("前端必须包含普通 index.html 和 version.json 文件")
		}
	}
	data, err := os.ReadFile(filepath.Join(app, "version.json"))
	var result struct {
		Version string `json:"version"`
	}
	if err != nil || json.Unmarshal(data, &result) != nil || result.Version != version {
		return errors.New("前端 version.json 与发行版本不一致")
	}
	return nil
}

func frontendFiles(root string) (map[string]string, error) {
	files := make(map[string]string)
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return errors.New("前端发布目录包含链接或特殊文件")
		}
		if info.IsDir() {
			files[filepath.ToSlash(relative)] = "directory"
			return nil
		}
		digest, err := hashFile(name)
		if err == nil {
			files[filepath.ToSlash(relative)] = digest
		}
		return err
	})
	return files, err
}
func sameFrontend(a, b string) (bool, error) {
	left, err := frontendFiles(a)
	if err != nil {
		return false, err
	}
	right, err := frontendFiles(b)
	if err != nil {
		return false, err
	}
	if len(left) != len(right) {
		return false, nil
	}
	for name, digest := range left {
		if right[name] != digest {
			return false, nil
		}
	}
	return true, nil
}

func publishFrontendAssets(app, root string) error {
	source := filepath.Join(app, "assets")
	if _, err := os.Lstat(source); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	files, err := frontendFiles(source)
	if err != nil {
		return errors.New("前端 assets 目录不合法")
	}
	destination := filepath.Join(root, "assets")
	if err := realDirectories(destination); err != nil {
		return err
	}
	var names []string
	for name, digest := range files {
		if digest == "directory" {
			continue
		}
		target := filepath.Join(destination, filepath.FromSlash(name))
		if err := realDirectories(filepath.Dir(target)); err != nil {
			return err
		}
		if info, err := os.Lstat(target); err == nil {
			if !info.Mode().IsRegular() {
				return errors.New("已发布 assets 不是普通文件")
			}
			existing, err := hashFile(target)
			if err != nil || existing != digest {
				return errors.New("assets 同名内容不同，拒绝覆盖旧资源")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return errors.New("无法检查已发布 assets")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		target := filepath.Join(destination, filepath.FromSlash(name))
		if err := os.Link(filepath.Join(source, filepath.FromSlash(name)), target); err != nil {
			if !errors.Is(err, os.ErrExist) {
				return errors.New("无法追加发布 assets")
			}
			info, statErr := os.Lstat(target)
			if statErr != nil || !info.Mode().IsRegular() {
				return errors.New("已发布 assets 不是普通文件")
			}
			existing, hashErr := hashFile(target)
			if hashErr != nil || existing != files[name] {
				return errors.New("assets 同名内容不同，拒绝覆盖旧资源")
			}
		}
	}
	return syncFrontendTree(destination)
}

func syncFrontendTree(root string) error {
	var directories []string
	if err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if err := os.Chmod(name, 0755); err != nil {
				return err
			}
			directories = append(directories, name)
		}
		return nil
	}); err != nil {
		return errors.New("无法同步前端目录")
	}
	for i := len(directories) - 1; i >= 0; i-- {
		if err := syncFrontendDirectory(directories[i]); err != nil {
			return err
		}
	}
	return nil
}
func syncFrontendDirectory(name string) error {
	directory, err := os.Open(name)
	if err != nil {
		return errors.New("无法打开前端目录")
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return errors.New("无法持久化前端目录")
	}
	return nil
}
