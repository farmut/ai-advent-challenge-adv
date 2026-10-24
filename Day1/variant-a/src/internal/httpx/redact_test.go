package httpx

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestRedactURLError_HidesSecretURL(t *testing.T) {
	inner := errors.New("connection refused")
	err := &url.Error{Op: "Get", URL: "https://api.example.com/botSECRET/getMe?appid=KEY", Err: inner}

	got := RedactURLError(err)

	if strings.Contains(got.Error(), "SECRET") || strings.Contains(got.Error(), "KEY") {
		t.Fatalf("secret leaked: %q", got.Error())
	}
	if !errors.Is(got, inner) {
		t.Fatalf("inner error lost: %v", got)
	}
}

func TestRedactURLError_PassesOtherErrors(t *testing.T) {
	err := errors.New("plain")
	if got := RedactURLError(err); got != err {
		t.Fatalf("got %v, want original error", got)
	}
}
