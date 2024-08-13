package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
	"net/http/httputil"
)

func TestHandler(t *testing.T) {
	target, _ := url.Parse("https://api.github.com")
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ModifyResponse = modifyResponse

	handler := handler(proxy)

	tests := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
	}{
		{"GET Request", "GET", "/", http.StatusOK},
		{"POST Request", "POST", "/", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, tt.path, nil)
			if err != nil {
				t.Fatal(err)
			}

			rr := httptest.NewRecorder()
			handler(rr, req)

			if status := rr.Code; status != tt.expectedStatus {
				t.Errorf("handler returned wrong status code: got %v want %v", status, tt.expectedStatus)
			}
		})
	}
}

func TestRateLimiting(t *testing.T) {
	target, _ := url.Parse("https://api.github.com")
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ModifyResponse = modifyResponse

	handler := handler(proxy)

	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	start := time.Now()
	for i := 0; i < 10; i++ {
		handler(rr, req)
	}
	duration := time.Since(start)

	if duration < time.Second {
		t.Errorf("Rate limiting not working as expected: 10 requests took %v, expected at least 1s", duration)
	}
}