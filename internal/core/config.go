package core

import (
	"fmt"
	"strconv"
	"strings"
)

type Config map[string]any

func (c Config) Has(key string) bool {
	v, ok := c[key]
	if !ok || v == nil {
		return false
	}
	if s, isStr := v.(string); isStr {
		return strings.TrimSpace(s) != ""
	}
	return true
}

func (c Config) String(key string) string {
	v, ok := c[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case bool:
		return strconv.FormatBool(t)
	default:
		return fmt.Sprint(t)
	}
}

func (c Config) StringOr(key, def string) string {
	if s := strings.TrimSpace(c.String(key)); s != "" {
		return s
	}
	return def
}

func (c Config) Int(key string, def int) int {
	v, ok := c[key]
	if !ok || v == nil {
		return def
	}
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		if t == "" {
			return def
		}
		if n, err := strconv.Atoi(strings.TrimSpace(t)); err == nil {
			return n
		}
	}
	return def
}

func (c Config) Int64(key string, def int64) int64 {
	v, ok := c[key]
	if !ok || v == nil {
		return def
	}
	switch t := v.(type) {
	case int:
		return int64(t)
	case int64:
		return t
	case float64:
		return int64(t)
	case string:
		if n, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64); err == nil {
			return n
		}
	}
	return def
}

func (c Config) Bool(key string, def bool) bool {
	v, ok := c[key]
	if !ok || v == nil {
		return def
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	case float64:
		return t != 0
	case int:
		return t != 0
	}
	return def
}

func (c Config) StringList(key string) []string {
	v, ok := c[key]
	if !ok || v == nil {
		return nil
	}
	var out []string
	switch t := v.(type) {
	case []string:
		for _, s := range t {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	case []any:
		for _, e := range t {
			if s := strings.TrimSpace(fmt.Sprint(e)); s != "" && e != nil {
				out = append(out, s)
			}
		}
	case string:
		for _, line := range strings.FieldsFunc(t, func(r rune) bool { return r == '\n' || r == ',' }) {
			if line = strings.TrimSpace(line); line != "" {
				out = append(out, line)
			}
		}
	}
	return out
}

func (c Config) Clone() Config {
	out := make(Config, len(c))
	for k, v := range c {
		out[k] = v
	}
	return out
}

func (c Config) Masked(secretFields []string) Config {
	out := c.Clone()
	for _, f := range secretFields {
		if out.Has(f) {
			out[f] = SecretMask
		}
	}
	return out
}

func (c Config) MergeSecrets(previous Config, secretFields []string) Config {
	out := c.Clone()
	for _, f := range secretFields {
		if out.String(f) == SecretMask {
			if prev, ok := previous[f]; ok {
				out[f] = prev
			} else {
				delete(out, f)
			}
		}
	}
	return out
}

func ValidateRequired(spec DriverSpec, cfg Config) error {
	var missing []string
	remote := cfg.Host() != nil
	for _, f := range spec.Fields {
		if !f.Required {
			continue
		}
		if remote && f.LocalOnly {
			continue
		}
		if !showIfMatches(f.ShowIf, cfg) {
			continue
		}
		if !cfg.Has(f.Name) {
			if f.Default != nil {
				continue
			}
			missing = append(missing, f.Label)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required fields: %s", strings.Join(missing, ", "))
	}
	return nil
}

func showIfMatches(showIf map[string]any, cfg Config) bool {
	if len(showIf) == 0 {
		return true
	}
	for k, want := range showIf {
		got := cfg.String(k)
		switch w := want.(type) {
		case []string:
			matched := false
			for _, s := range w {
				if s == got {
					matched = true
				}
			}
			if !matched {
				return false
			}
		case []any:
			matched := false
			for _, s := range w {
				if fmt.Sprint(s) == got {
					matched = true
				}
			}
			if !matched {
				return false
			}
		case bool:
			if cfg.Bool(k, false) != w {
				return false
			}
		default:
			if fmt.Sprint(w) != got {
				return false
			}
		}
	}
	return true
}
