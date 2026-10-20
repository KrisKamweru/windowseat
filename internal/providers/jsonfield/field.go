// Package jsonfield decodes provider response fields one at a time.
package jsonfield

import "encoding/json"

// Field decodes one provider field on its own. A wrongly typed value marks
// the field Unreadable instead of failing the whole response, so a parser
// can report that field as an issue and keep the rest.
type Field[T any] struct {
	Value      *T
	Unreadable bool
}

func (f *Field[T]) UnmarshalJSON(raw []byte) error {
	if string(raw) == "null" {
		return nil
	}
	var value T
	if json.Unmarshal(raw, &value) != nil {
		f.Unreadable = true
		return nil
	}
	f.Value = &value
	return nil
}

// OrZero returns the decoded value, or the zero value when the field was
// absent, null or unreadable.
func (f Field[T]) OrZero() T {
	if f.Value == nil {
		var zero T
		return zero
	}
	return *f.Value
}
