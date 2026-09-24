package main

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

func env(k, fallback string) string {
	if v, ok := os.LookupEnv(k); ok {
		return v
	}
	return fallback
}
func loadConfig() {
	file, err := os.Open(env("VOEX_ENV_FILE", ".env"))
	if os.IsNotExist(err) {
		return
	}
	must(err)
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
			must(e)
			v = x
		}
		if _, ok := os.LookupEnv(k); !ok {
			must(os.Setenv(k, v))
		}
	}
	must(scanner.Err())
}
