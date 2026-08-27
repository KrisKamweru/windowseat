package versionprofile

import (
	"errors"
	"testing"
)

func TestSelectUsesNewestEligibleProfile(t *testing.T) {
	profiles := MustNew([]Definition[string]{
		{ID: "first", PreferredFrom: "2.1.245", Value: "parser-a"},
		{ID: "second", PreferredFrom: "2.1.310", Value: "parser-b"},
		{ID: "third", PreferredFrom: "2.2.0", Value: "parser-c"},
	})

	tests := map[string]struct {
		id              string
		value           string
		beforePreferred bool
	}{
		"2.1.100": {id: "first", value: "parser-a", beforePreferred: true},
		"2.1.245": {id: "first", value: "parser-a"},
		"2.1.309": {id: "first", value: "parser-a"},
		"2.1.310": {id: "second", value: "parser-b"},
		"2.1.999": {id: "second", value: "parser-b"},
		"2.2.0":   {id: "third", value: "parser-c"},
		"3.0.0":   {id: "third", value: "parser-c"},
	}

	for observed, want := range tests {
		t.Run(observed, func(t *testing.T) {
			selection, err := profiles.Select(observed)
			if err != nil {
				t.Fatalf("Select() error = %v", err)
			}
			if selection.ID != want.id || selection.Value != want.value ||
				selection.BeforePreferred != want.beforePreferred {
				t.Fatalf("Select() = %#v", selection)
			}
		})
	}
}

func TestNewRejectsUnsortedAndDuplicateProfiles(t *testing.T) {
	tests := map[string][]Definition[string]{
		"unsorted": {
			{ID: "new", PreferredFrom: "2.2.0"},
			{ID: "old", PreferredFrom: "2.1.245"},
		},
		"duplicate": {
			{ID: "a", PreferredFrom: "2.1.245"},
			{ID: "b", PreferredFrom: "2.1.245"},
		},
		"duplicate_id": {
			{ID: "same", PreferredFrom: "2.1.245"},
			{ID: "same", PreferredFrom: "2.1.310"},
		},
	}

	for name, definitions := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := New(definitions); err == nil {
				t.Fatal("New() error = nil")
			}
		})
	}
}

func TestSelectRejectsMalformedProviderVersion(t *testing.T) {
	profiles := MustNew([]Definition[string]{{ID: "first", PreferredFrom: "1.0.0"}})

	_, err := profiles.Select("GitHub Copilot CLI 1.0.80")
	if !errors.Is(err, ErrInvalidVersion) {
		t.Fatalf("Select() error = %v, want ErrInvalidVersion", err)
	}
}

func TestZeroSetCannotSelect(t *testing.T) {
	var profiles Set[string]

	if _, err := profiles.Select("1.0.0"); err == nil {
		t.Fatal("Select() error = nil")
	}
}

func TestVersionOrderingCoversProviderVersionShapes(t *testing.T) {
	tests := map[string]struct {
		profiles []Definition[string]
		observed string
		want     string
	}{
		"claude": {
			profiles: []Definition[string]{
				{ID: "claude-2.1", PreferredFrom: "2.1.245", Value: "old"},
				{ID: "claude-2.2", PreferredFrom: "2.2.0", Value: "new"},
			},
			observed: "2.1.300",
			want:     "old",
		},
		"codex": {
			profiles: []Definition[string]{
				{ID: "codex-0.149", PreferredFrom: "0.149.1", Value: "old"},
				{ID: "codex-0.160", PreferredFrom: "0.160.0", Value: "new"},
			},
			observed: "0.160.2",
			want:     "new",
		},
		"copilot": {
			profiles: []Definition[string]{
				{ID: "copilot-1.0", PreferredFrom: "1.0.78", Value: "old"},
				{ID: "copilot-1.1", PreferredFrom: "1.1.0", Value: "new"},
			},
			observed: "1.0.80",
			want:     "old",
		},
	}

	for provider, test := range tests {
		t.Run(provider, func(t *testing.T) {
			profiles := MustNew(test.profiles)
			selection, err := profiles.Select(test.observed)
			if err != nil {
				t.Fatalf("Select(%q) error = %v", test.observed, err)
			}
			if selection.Value != test.want {
				t.Fatalf("Select(%q).Value = %q, want %q", test.observed, selection.Value, test.want)
			}
		})
	}
}
