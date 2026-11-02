package i18n

import (
	"regexp"
	"slices"
	"testing"
)

var verb = regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z%]`)

// A translation with different format verbs would garble output at runtime.
func TestFormatVerbsMatch(t *testing.T) {
	for en, tr := range pt {
		if tr == "" {
			t.Errorf("%q: empty translation", en)
			continue
		}
		a, b := verb.FindAllString(en, -1), verb.FindAllString(tr, -1)
		if !slices.Equal(a, b) {
			t.Errorf("%q: verbs %v vs %v in %q", en, a, b, tr)
		}
	}
}

func TestParse(t *testing.T) {
	for in, want := range map[string]Lang{"pt_BR.UTF-8": PT, "pt-br": PT, "PT": PT, "português": PT, "en_US.UTF-8": EN, "english": EN} {
		got, ok := Parse(in)
		if !ok || got != want {
			t.Errorf("Parse(%q) = %v, %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "C.UTF-8", "fr_FR"} {
		if _, ok := Parse(in); ok {
			t.Errorf("Parse(%q) should be undetermined", in)
		}
	}
}

func TestInitConsumesFlagAndTranslates(t *testing.T) {
	defer Set(EN)
	rest := Init([]string{"lights", "--lang", "pt-br", "off"})
	if !slices.Equal(rest, []string{"lights", "off"}) || Current() != PT {
		t.Fatalf("rest=%v lang=%v", rest, Current())
	}
	if T("Lights") != "Luzes" || Tf("Sidetone at %d%%", 40) != "Sidetone em 40%" {
		t.Fatal("translation not applied")
	}
	Set(EN)
	if T("Lights") != "Lights" || T("not in catalog") != "not in catalog" {
		t.Fatal("English must pass through")
	}
}
