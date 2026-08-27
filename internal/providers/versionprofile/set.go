package versionprofile

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var ErrInvalidVersion = errors.New("invalid provider version")

type Definition[T any] struct {
	ID            string
	PreferredFrom string
	Value         T
}

type Selection[T any] struct {
	ID              string
	PreferredFrom   string
	BeforePreferred bool
	Value           T
}

type Set[T any] struct {
	profiles []profile[T]
}

type profile[T any] struct {
	id            string
	preferredFrom string
	version       version
	value         T
}

type version struct {
	major int
	minor int
	patch int
}

func New[T any](definitions []Definition[T]) (Set[T], error) {
	if len(definitions) == 0 {
		return Set[T]{}, errors.New("version profiles are empty")
	}

	profiles := make([]profile[T], len(definitions))
	ids := make(map[string]struct{}, len(definitions))
	for index, definition := range definitions {
		if strings.TrimSpace(definition.ID) == "" {
			return Set[T]{}, fmt.Errorf("version profile %d has no ID", index)
		}
		if _, exists := ids[definition.ID]; exists {
			return Set[T]{}, fmt.Errorf("version profile ID %q is duplicated", definition.ID)
		}
		ids[definition.ID] = struct{}{}
		parsed, err := parseVersion(definition.PreferredFrom)
		if err != nil {
			return Set[T]{}, fmt.Errorf("version profile %q: %w", definition.ID, err)
		}
		if index > 0 && !profiles[index-1].version.less(parsed) {
			return Set[T]{}, errors.New("version profiles must have unique ascending preferred versions")
		}
		profiles[index] = profile[T]{
			id:            definition.ID,
			preferredFrom: definition.PreferredFrom,
			version:       parsed,
			value:         definition.Value,
		}
	}
	return Set[T]{profiles: profiles}, nil
}

func MustNew[T any](definitions []Definition[T]) Set[T] {
	profiles, err := New(definitions)
	if err != nil {
		panic(err)
	}
	return profiles
}

func (set Set[T]) Select(rawVersion string) (Selection[T], error) {
	if len(set.profiles) == 0 {
		return Selection[T]{}, errors.New("version profiles are empty")
	}
	observed, err := parseVersion(rawVersion)
	if err != nil {
		return Selection[T]{}, err
	}

	selected := set.profiles[0]
	for index := len(set.profiles) - 1; index >= 0; index-- {
		candidate := set.profiles[index]
		if !observed.less(candidate.version) {
			selected = candidate
			break
		}
	}

	return Selection[T]{
		ID:              selected.id,
		PreferredFrom:   selected.preferredFrom,
		BeforePreferred: observed.less(set.profiles[0].version),
		Value:           selected.value,
	}, nil
}

func parseVersion(raw string) (version, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return version{}, fmt.Errorf("%w: %q", ErrInvalidVersion, raw)
	}

	values := [3]int{}
	for index, part := range parts {
		if part == "" || strings.TrimLeft(part, "0123456789") != "" {
			return version{}, fmt.Errorf("%w: %q", ErrInvalidVersion, raw)
		}
		value, err := strconv.Atoi(part)
		if err != nil {
			return version{}, fmt.Errorf("%w: %q", ErrInvalidVersion, raw)
		}
		values[index] = value
	}
	return version{major: values[0], minor: values[1], patch: values[2]}, nil
}

func (left version) less(right version) bool {
	if left.major != right.major {
		return left.major < right.major
	}
	if left.minor != right.minor {
		return left.minor < right.minor
	}
	return left.patch < right.patch
}
