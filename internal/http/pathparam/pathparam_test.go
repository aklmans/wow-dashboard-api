package pathparam_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/aklmans/wow-dashboard-api/internal/http/pathparam"
)

func TestParseUUIDValid(t *testing.T) {
	want := uuid.New()
	got, err := pathparam.ParseUUID("  "+want.String()+"  ", "id")
	if err != nil {
		t.Fatalf("ParseUUID returned error: %v", err)
	}
	if got != want {
		t.Fatalf("ParseUUID = %s, want %s", got, want)
	}
}

func TestParseUUIDEmptyReturnsInvalid(t *testing.T) {
	cases := []string{"", "   ", "\t\n"}
	for _, in := range cases {
		_, err := pathparam.ParseUUID(in, "id")
		if !errors.Is(err, pathparam.ErrInvalidUUID) {
			t.Fatalf("ParseUUID(%q) err = %v, want ErrInvalidUUID", in, err)
		}
		if !strings.Contains(err.Error(), "id is required") {
			t.Fatalf("ParseUUID(%q) err = %v, want field-aware message", in, err)
		}
	}
}

func TestParseUUIDMalformedReturnsInvalid(t *testing.T) {
	cases := []string{"not-a-uuid", "12345", "00000000-0000-0000-0000-00000000000z"}
	for _, in := range cases {
		_, err := pathparam.ParseUUID(in, "id")
		if !errors.Is(err, pathparam.ErrInvalidUUID) {
			t.Fatalf("ParseUUID(%q) err = %v, want ErrInvalidUUID", in, err)
		}
		if !strings.Contains(err.Error(), "must be a valid UUID") {
			t.Fatalf("ParseUUID(%q) err = %v, want field-aware message", in, err)
		}
	}
}

func TestParseUUIDDefaultsFieldName(t *testing.T) {
	_, err := pathparam.ParseUUID("", "")
	if err == nil || !strings.Contains(err.Error(), "value is required") {
		t.Fatalf("ParseUUID empty field err = %v, want default field name", err)
	}
}
