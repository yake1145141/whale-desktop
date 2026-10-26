package credentials

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Result reports a credential and the source it came from.
type Result struct {
	Value  string `json:"value"`
	Source string `json:"source"`
}

// APIClientKey resolves the DeepSeek API key.
func APIClientKey(configValue string) Result {
	return resolve(configValue, "DEEPSEEK_API_KEY")
}

// PlatformToken resolves the platform web-session token.
func PlatformToken(configValue string) Result {
	return resolve(configValue, "DEEPSEEK_PLATFORM_TOKEN")
}

func resolve(configValue, envName string) Result {
	if value := strings.TrimSpace(configValue); value != "" {
		return Result{Value: value, Source: "config.jsonc"}
	}
	if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
		return Result{Value: value, Source: "env:" + envName}
	}
	if value := ReadDSH(envName); value != "" {
		value = strings.TrimSpace(regexp.MustCompile(`(?i)^Bearer\s+`).ReplaceAllString(value, ""))
		return Result{Value: value, Source: "DSH 凭据"}
	}
	return Result{Source: "none"}
}

// ReadDSH reads one scalar from the refs block of .credentials.yaml.
func ReadDSH(name string) string {
	home := os.Getenv("DSH_HOME")
	if home == "" {
		userHome, _ := os.UserHomeDir()
		home = filepath.Join(userHome, ".dsh")
	}
	data, err := os.ReadFile(filepath.Join(home, ".credentials.yaml"))
	if err != nil {
		return ""
	}
	inRefs, refsIndent := false, -1
	for _, raw := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " \t"))
		if !inRefs {
			if strings.HasPrefix(trimmed, "refs") && strings.Contains(trimmed, ":") {
				after := strings.TrimSpace(strings.SplitN(trimmed, ":", 2)[1])
				if after == "" {
					inRefs, refsIndent = true, indent
				}
			}
			continue
		}
		if indent <= refsIndent && !strings.HasPrefix(trimmed, "-") {
			return ""
		}
		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) != name {
			continue
		}
		value := strings.TrimSpace(parts[1])
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') ||
			(value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		if value == "" || value == "~" || value == "null" {
			return ""
		}
		return value
	}
	return ""
}
