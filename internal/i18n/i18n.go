// Package i18n holds the interface language: English (default) or Brazilian
// Portuguese. Source strings are written in English and double as lookup
// keys, so a missing translation simply falls back to English.
package i18n

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

// Lang is a supported interface language.
type Lang string

const (
	EN Lang = "en"
	PT Lang = "pt-BR"
)

var current atomic.Value // Lang

func init() { current.Store(EN) }

// Current returns the active language.
func Current() Lang {
	if l, ok := current.Load().(Lang); ok {
		return l
	}
	return EN
}

// Set changes the active language.
func Set(l Lang) { current.Store(l) }

// Toggle switches between English and Portuguese and returns the new one.
func Toggle() Lang {
	next := PT
	if Current() == PT {
		next = EN
	}
	Set(next)
	return next
}

// Name is the language's own name.
func (l Lang) Name() string {
	if l == PT {
		return "Português (Brasil)"
	}
	return "English"
}

// Parse accepts en, english, pt, pt-br, pt_BR.UTF-8, portugues...
func Parse(s string) (Lang, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case s == "":
		return EN, false
	case strings.HasPrefix(s, "pt"), strings.HasPrefix(s, "portug"):
		return PT, true
	case strings.HasPrefix(s, "en"):
		return EN, true
	}
	return EN, false
}

func configFile() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "g733", "lang"), nil
}

// Save remembers l as the preferred language.
func Save(l Lang) error {
	path, err := configFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(string(l)+"\n"), 0o644)
}

func saved() (Lang, bool) {
	path, err := configFile()
	if err != nil {
		return EN, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return EN, false
	}
	return Parse(string(b))
}

// Detect picks the language: $G733_LANG, then the saved preference, then
// $LC_ALL / $LC_MESSAGES / $LANG, then English.
func Detect() Lang {
	if l, ok := Parse(os.Getenv("G733_LANG")); ok {
		return l
	}
	if l, ok := saved(); ok {
		return l
	}
	for _, v := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if l, ok := Parse(os.Getenv(v)); ok {
			return l
		}
	}
	return EN
}

// Init sets the language from a --lang flag (removed from args) or Detect.
func Init(args []string) []string {
	out := make([]string, 0, len(args))
	lang, explicit := EN, false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--lang" && i+1 < len(args):
			lang, explicit = Parse(args[i+1])
			i++
		case strings.HasPrefix(a, "--lang="):
			lang, explicit = Parse(strings.TrimPrefix(a, "--lang="))
		default:
			out = append(out, a)
		}
	}
	if !explicit {
		lang = Detect()
	}
	Set(lang)
	return out
}

// T translates an English source string.
func T(s string) string { return In(Current(), s) }

// In translates s into a specific language, regardless of the active one.
func In(l Lang, s string) string {
	if l == PT {
		if t, ok := pt[s]; ok {
			return t
		}
	}
	return s
}

// Tf translates a format string and applies it.
func Tf(format string, args ...any) string { return fmt.Sprintf(T(format), args...) }

// Errorf is fmt.Errorf with a translated format.
func Errorf(format string, args ...any) error { return fmt.Errorf(T(format), args...) }
