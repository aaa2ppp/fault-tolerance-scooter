package env

import (
	"fmt"
	"os"
	"strings"
)

var Debug = OnEnv("DEBUG")

// OnEnv читает булев флаг из окружения.
// Если переменная не задана (или задана пустой/некорректной),
// возвращается def (по умолчанию false).
func OnEnv(name string, def ...bool) bool {
	d := false
	if len(def) > 0 {
		d = def[0]
	}
	val, ok := os.LookupEnv(name)
	if !ok || strings.TrimSpace(val) == "" {
		return d
	}
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "1", "yes", "on", "true", "enable", "enabled":
		return true
	case "0", "no", "off", "false", "disable", "disabled":
		return false
	default:
		fmt.Fprintf(os.Stderr, "env %s=%q: ignored unknown value; want 0/1, yes/no, on/off, true/false\n", name, val)
		return d
	}
}
