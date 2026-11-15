// Package config — чтение настроек из окружения.
package config

import (
	"os"
	"strings"
)

// Env возвращает значение переменной окружения или значение по умолчанию.
func Env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// EnvList возвращает переменную окружения как список (разделитель — запятая).
func EnvList(key, def string) []string {
	raw := Env(key, def)
	out := []string{}
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
