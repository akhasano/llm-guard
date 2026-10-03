package detectors

import (
	"reflect"
	"testing"
)

func TestTermsDetector(t *testing.T) {
	tests := []struct {
		name  string
		terms []string
		text  string
		want  []string
	}{
		{"Cyrillic and repeated", []string{"ООО Ромашка"}, "До: ооо РОМАШКА; ООО Ромашка.", []string{"ооо РОМАШКА", "ООО Ромашка"}},
		{"literal regex characters", []string{"a.b+[x]"}, "aXbbx a.b+[x]", []string{"a.b+[x]"}},
		{"substring", []string{"secret"}, "SECRETsecretive", []string{"SECRET", "secret"}},
		{"exact whitespace", []string{"Лесная, д. 10"}, "Лесная,  д. 10", nil},
		{"overlapping and duplicate", []string{"abc", "bcde", "abc"}, "abcde", []string{"abcde"}},
		{"longer same start", []string{"Проект", "Проект Аврора"}, "Проект Аврора", []string{"Проект Аврора"}},
		{"empty list", nil, "some text", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := NewTermsDetector(tt.terms)
			if err != nil {
				t.Fatal(err)
			}
			if d.Name() != "terms" {
				t.Fatal("unexpected detector name")
			}
			var got []string
			for _, m := range d.Detect(tt.text) {
				if m.Category != "custom_term" || tt.text[m.Start:m.End] != m.Value {
					t.Fatal("invalid match category or byte offsets")
				}
				got = append(got, m.Value)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTermsDetectorRejectsBlankEntries(t *testing.T) {
	for _, term := range []string{"", " ", "\t\n"} {
		if _, err := NewTermsDetector([]string{term}); err == nil {
			t.Fatal("expected blank term error")
		}
	}
}
