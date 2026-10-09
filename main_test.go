package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPortFromEnv(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "", want: "8080"},
		{in: "9090", want: "9090"},
		{in: "1", want: "1"},
		{in: "65535", want: "65535"},
		{in: "0", wantErr: true},
		{in: "65536", wantErr: true},
		{in: "-1", wantErr: true},
		{in: "http", wantErr: true},
		{in: " 8080", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := portFromEnv(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("portFromEnv(%q) = %q, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("portFromEnv(%q) unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("portFromEnv(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestHandler(t *testing.T) {
	srv := httptest.NewServer(newMux())
	t.Cleanup(srv.Close)

	tests := []struct {
		name     string
		method   string
		path     string
		wantCode int
		wantBody string
	}{
		{name: "root", method: http.MethodGet, path: "/", wantCode: http.StatusOK, wantBody: greeting + "\n"},
		{name: "unknown path", method: http.MethodGet, path: "/nope", wantCode: http.StatusNotFound},
		{name: "wrong method", method: http.MethodPost, path: "/", wantCode: http.StatusMethodNotAllowed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(t.Context(), tc.method, srv.URL+tc.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tc.wantCode {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantCode)
			}
			if tc.wantBody == "" {
				return
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != tc.wantBody {
				t.Fatalf("body = %q, want %q", body, tc.wantBody)
			}
		})
	}
}
