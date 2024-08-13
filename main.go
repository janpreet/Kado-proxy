package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt"
	"golang.org/x/time/rate"
)

const (
	githubAPIURL = "https://api.github.com"
)

var (
	limiter        = rate.NewLimiter(rate.Every(time.Hour/5000), 1) // 5000 requests per hour
	githubAppID    string
	githubAppKey   []byte
	installationID int64
	certFile       string
	keyFile        string
	port           int
)

type RateLimitResponse struct {
	Resources struct {
		Core struct {
			Limit     int `json:"limit"`
			Remaining int `json:"remaining"`
			Reset     int `json:"reset"`
		} `json:"core"`
	} `json:"resources"`
}

func init() {
	githubAppID = os.Getenv("GITHUB_APP_ID")
	githubAppKey = []byte(os.Getenv("GITHUB_APP_PRIVATE_KEY"))
	installationID, _ = strconv.ParseInt(os.Getenv("GITHUB_INSTALLATION_ID"), 10, 64)
	flag.StringVar(&certFile, "cert", "", "Path to TLS certificate file")
	flag.StringVar(&keyFile, "key", "", "Path to TLS key file")
	flag.IntVar(&port, "port", 8443, "Port to run the server on")
}

func main() {
	flag.Parse()

	if certFile == "" || keyFile == "" {
		log.Fatal("TLS certificate and key files are required")
	}

	target, err := url.Parse(githubAPIURL)
	if err != nil {
		log.Fatal(err)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ModifyResponse = modifyResponse

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: http.HandlerFunc(handler(proxy)),
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}

	log.Printf("Starting kado-proxy HTTPS server on :%d\n", port)
	log.Fatal(server.ListenAndServeTLS(certFile, keyFile))
}

func generateJWT() (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(10 * time.Minute).Unix(),
		"iss": githubAppID,
	})

	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM(githubAppKey)
	if err != nil {
		return "", err
	}

	return token.SignedString(privateKey)
}

func getInstallationToken(jwt string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/app/installations/%d/access_tokens", installationID)
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	token, ok := result["token"].(string)
	if !ok {
		return "", fmt.Errorf("unable to get installation token")
	}

	return token, nil
}

func handler(p *httputil.ReverseProxy) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := limiter.Wait(r.Context()); err != nil {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		// Check if we're using a GitHub App
		if githubAppID != "" && len(githubAppKey) > 0 {
			jwt, err := generateJWT()
			if err != nil {
				http.Error(w, "Failed to generate JWT", http.StatusInternalServerError)
				return
			}

			token, err := getInstallationToken(jwt)
			if err != nil {
				http.Error(w, "Failed to get installation token", http.StatusInternalServerError)
				return
			}

			r.Header.Set("Authorization", "token "+token)
		} else if auth := r.Header.Get("Authorization"); auth != "" {
			// If not using a GitHub App, forward the existing Authorization header
			r.Header.Set("Authorization", auth)
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