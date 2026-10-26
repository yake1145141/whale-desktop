// Package jsonc provides the small JSONC and map-merging helpers used by the
// application. The parser accepts comments and trailing commas while keeping
// strings such as URLs intact.
package jsonc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Strip removes line comments, block comments, and trailing commas from JSONC.
func Strip(text string) string {
	var out strings.Builder
	out.Grow(len(text))
	inString, escaped, inLine, inBlock := false, false, false, false
	for i := 0; i < len(text); i++ {
		c, next := text[i], byte(0)
		if i+1 < len(text) {
			next = text[i+1]
		}
		if inLine {
			if c == '\n' {
				inLine = false
				out.WriteByte(c)
			}
			continue
		}
		if inBlock {
			if c == '*' && next == '/' {
				inBlock = false
				i++
			}
			continue
		}
		if inString {
			out.WriteByte(c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			out.WriteByte(c)
			continue
		}
		if c == '/' && next == '/' {
			inLine = true
			i++
			continue
		}
		if c == '/' && next == '*' {
			inBlock = true
			i++
			continue
		}
		out.WriteByte(c)
	}

	clean := strings.TrimPrefix(out.String(), "\ufeff")
	var result strings.Builder
	result.Grow(len(clean))
	for i := 0; i < len(clean); i++ {
		if clean[i] == ',' {
			j := i + 1
			for j < len(clean) && (clean[j] == ' ' || clean[j] == '\t' || clean[j] == '\r' || clean[j] == '\n') {
				j++
			}
			if j < len(clean) && (clean[j] == '}' || clean[j] == ']') {
				continue
			}
		}
		result.WriteByte(clean[i])
	}
	return result.String()
}

// Parse parses JSONC into a generic map. It does not throw.
func Parse(text string) (map[string]any, error) {
	dec := json.NewDecoder(strings.NewReader(Strip(text)))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("根节点必须是 JSON 对象")
	}
	return obj, nil
}

// ReadResult is the result of reading a user-editable JSONC file.
type ReadResult struct {
	Value   map[string]any
	Error   error
	Missing bool
	Text    string
}

// Read parses a JSONC file.
func Read(file string) ReadResult {
	data, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return ReadResult{Missing: true}
		}
		return ReadResult{Error: err}
	}
	text := strings.TrimPrefix(string(data), "\ufeff")
	value, parseErr := Parse(text)
	return ReadResult{Value: value, Error: parseErr, Text: text}
}

// WriteAtomic writes indented JSON via a temporary file and rename.
func WriteAtomic(file string, value any) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp-%d", file, os.Getpid())
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, file); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// WriteTextAtomic atomically writes UTF-8 text.
func WriteTextAtomic(file, text string) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp-%d", file, os.Getpid())
	if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, file); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// MarshalIndented renders a value as two-space-indented JSON.
func MarshalIndented(value any) (string, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// DeepMerge overlays patch on base. Nested objects merge recursively; arrays are
// replaced wholesale. The inputs are not modified.
func DeepMerge(base, patch map[string]any) map[string]any {
	out := Clone(base)
	for key, pv := range patch {
		pObj, pOK := pv.(map[string]any)
		bObj, bOK := out[key].(map[string]any)
		if pOK && bOK {
			out[key] = DeepMerge(bObj, pObj)
		} else {
			out[key] = cloneValue(pv)
		}
	}
	return out
}

// Clone returns a deep copy of a generic JSON object.
func Clone(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(value))
	for key, v := range value {
		out[key] = cloneValue(v)
	}
	return out
}

func cloneValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		return Clone(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = cloneValue(item)
		}
		return out
	default:
		return typed
	}
}

// Decode converts a generic object into a concrete Go value.
func Decode(value any, target any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

// MarshalMap converts a concrete value into a generic JSON object.
func MarshalMap(value any) (map[string]any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var out map[string]any
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// ExpandHome expands a leading ~.
func ExpandHome(path, home string) string {
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		return filepath.Join(home, path[2:])
	}
	return path
}
