package env

import (
	"fmt"
	"os"
	"strings"
)

var Debug = OnEnv("DEBUG")

func OnEnv(name string) bool {
	val := os.Getenv(name)
	switch strings.ToLower(val) {
	case "1", "yes", "on", "true", "enable":
		return true
	case "0", "no", "off", "false", "disable", "":
		return false
	default:
		fmt.Fprintf(os.Stderr, "env %s=%q: ignored unknown value; want 0/1, yes/no, on/off, true/false\n", name, val)
		return false
	}
}
