package confucius

import (
	"fmt"
	"testing"
)

func Test_fieldErrors_Error(t *testing.T) {
	fe := make(fieldErrors)

	fe["B"] = fmt.Errorf("berr")
	fe["A"] = fmt.Errorf("aerr")

	got := fe.Error()

	want := "A: aerr, B: berr"
	if want != got {
		t.Fatalf("want %q, got %q", want, got)
	}

	fe = make(fieldErrors)
	got = fe.Error()

	if got != "" {
		t.Fatalf("empty errors returned non-empty string: %s", got)
	}
}
