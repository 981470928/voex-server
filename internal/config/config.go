package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"voex-server/internal/shared"
)

func Env(k, fallback string) string {
	if v, ok := os.LookupEnv(k); ok {
		return v
	}
	return fallback
}
func Load() {
	file, err := os.Open(Env("VOEX_ENV_FILE", ".env"))
	if os.IsNotExist(err) {
		return
	}
	shared.Must(err)
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if strings.HasPrefix(v, "\"") {
			x, e := strconv.Unquote(v)
			shared.Must(e)
			v = x
		}
		if _, ok := os.LookupEnv(k); !ok {
			shared.Must(os.Setenv(k, v))
		}
	}
	shared.Must(scanner.Err())
}

// Config groups application settings; secrets are loaded from the environment.
type Config struct {
	Address, StaticDir, UploadDir, SecretFile string
	Origins                                   []string
	Database                                  Database
}
type Database struct{ Host, Port, User, Password, Name string }

func Read() Config {
	Load()
	return Config{Address: Env("VOEX_ADDR", "127.0.0.1:8090"), StaticDir: Env("VOEX_STATIC_DIR", "/home/static"), UploadDir: Env("VOEX_UPLOAD_DIR", "/home/update"), SecretFile: Env("JWT_SECRET_FILE", "/home/server/.secrets/jwt-key"), Origins: strings.Split(Env("AUTH_ALLOWED_ORIGINS", "https://voex.jmin.site,https://jmin.site,http://218.76.62.176,http://localhost:3000,http://127.0.0.1:3000,http://localhost:3001,http://127.0.0.1:3001"), ","), Database: Database{Host: Env("MYSQL_HOST", "127.0.0.1"), Port: Env("MYSQL_PORT", "3306"), User: Env("MYSQL_USER", "root"), Password: Env("MYSQL_PASSWORD", ""), Name: Env("MYSQL_DATABASE", "file_server")}}
}
