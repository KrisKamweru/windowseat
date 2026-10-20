package jsonfield

import (
	"encoding/json"
	"testing"
)

func TestFieldDecodesEachValueOnItsOwn(t *testing.T) {
	var got struct {
		Good    Field[float64] `json:"good"`
		Wrong   Field[float64] `json:"wrong"`
		Null    Field[float64] `json:"null"`
		Missing Field[float64] `json:"missing"`
	}
	if err := json.Unmarshal([]byte(`{"good": 42, "wrong": "42", "null": null}`), &got); err != nil {
		t.Fatal(err)
	}
	if got.Good.OrZero() != 42 || got.Good.Unreadable {
		t.Errorf("good = %+v", got.Good)
	}
	if !got.Wrong.Unreadable || got.Wrong.Value != nil {
		t.Errorf("wrong = %+v", got.Wrong)
	}
	for name, f := range map[string]Field[float64]{"null": got.Null, "missing": got.Missing} {
		if f.Unreadable || f.Value != nil || f.OrZero() != 0 {
			t.Errorf("%s = %+v", name, f)
		}
	}
}
