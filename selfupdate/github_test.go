package selfupdate

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type releaseTransport func(*http.Request) (*http.Response, error)

func (f releaseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Online self-update is compiled out in this fork (Enabled == false). The two
// online entry points must refuse before touching the network.

func TestFetchLatestDisabledMakesNoRequest(t *testing.T) {
	if Enabled {
		t.Skip("online self-update is enabled in this build; disabled-path test does not apply")
	}
	called := false
	client := &http.Client{Transport: releaseTransport(func(*http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	_, err := FetchLatest(context.Background(), client)
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("want ErrDisabled, got %v", err)
	}
	if called {
		t.Fatal("FetchLatest must not make a network request when self-update is disabled")
	}
}

func TestStageDisabledMakesNoRequest(t *testing.T) {
	if Enabled {
		t.Skip("online self-update is enabled in this build; disabled-path test does not apply")
	}
	called := false
	client := &http.Client{Transport: releaseTransport(func(*http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	rel := &Release{TagName: "v9.9.9", Assets: []Asset{{Name: "SHA256SUMS", URL: "https://example.invalid/SHA256SUMS"}}}
	err := Stage(context.Background(), client, rel, "v0.0.1", nil)
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("want ErrDisabled, got %v", err)
	}
	if called {
		t.Fatal("Stage must not make a network request when self-update is disabled")
	}
}
