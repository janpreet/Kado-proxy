package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"golang.org/x/time/rate"
)

const (
	githubAPIURL = "https://api.github.com"
	proxyPort    = 8080
)

var limiter = rate.NewLimiter(rate.Every(time.Hour/5000), 1) // 5000 requests per hour

type RateLimitResponse struct {
	Resources struct {
		Core struct {
			Limit     int `json:"limit"`
			Remaining int `json:"remaining"`
			Reset     int `json:"reset"`
		} `json:"core"`
	} `json:"resources"`
}

func main() {
	target, err := url.Parse(githubAPIURL)
	if err != nil {
		log.Fatal(err)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ModifyResponse = modifyResponse

	http.HandleFunc("/", handler(proxy))
	log.Printf("Starting kado-proxy server on :%d\n", proxyPort)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", proxyPort), nil))
}

func handler(p *httputil.ReverseProxy) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := limiter.Wait(r.Context()); err != nil {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		r.Host = "api.github.com"
		p.ServeHTTP(w, r)
	}
}

func modifyResponse(r *http.Response) error {
	if r.StatusCode == http.StatusForbidden {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return err
		}
		r.Body.Close()

		var rateLimitResp RateLimitResponse
		if err := json.Unmarshal(body, &rateLimitResp); err != nil {
			return err
		}

		remaining := rateLimitResp.Resources.Core.Remaining
		reset := time.Unix(int64(rateLimitResp.Resources.Core.Reset), 0)
		waitTime := time.Until(reset)

		log.Printf("Rate limit reached. Remaining: %d, Reset: %s, Waiting: %s\n", remaining, reset, waitTime)

		if remaining == 0 {
			time.Sleep(waitTime)
		}

		r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), r.Body))
	}

	return nil
}