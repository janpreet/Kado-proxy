package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
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

type Config struct {
	GithubAppID    string
	GithubAppKey   []byte
	InstallationID int64
	CertFile       string
	KeyFile        string
	Port           int
}

func loadConfig() (*Config, error) {
	flag.StringVar(&certFile, "cert", "", "Path to TLS certificate file")
	flag.StringVar(&keyFile, "key", "", "Path to TLS key file")
	flag.IntVar(&port, "port", 8443, "Port to run the server on")
	flag.Parse()

	githubAppID = os.Getenv("GITHUB_APP_ID")
	githubAppKey = []byte(os.Getenv("GITHUB_APP_PRIVATE_KEY"))
	installationID, _ = strconv.ParseInt(os.Getenv("GITHUB_INSTALLATION_ID"), 10, 64)

	if certFile == "" || keyFile == "" {
		return nil, fmt.Errorf("TLS certificate and key files are required")
	}

	return &Config{
		GithubAppID:    githubAppID,
		GithubAppKey:   githubAppKey,
		InstallationID: installationID,
		CertFile:       certFile,
		KeyFile:        keyFile,
		Port:           port,
	}, nil
}

func generateJWT(appID string, privateKey []byte) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(10 * time.Minute).Unix(),
		"iss": appID,
	})

	key, err := jwt.ParseRSAPrivateKeyFromPEM(privateKey)
	if err != nil {
		return "", err
	}

	return token.SignedString(key)
}

func getInstallationToken(jwt string, installationID int64) (string, error) {
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

	body, err := io.ReadAll(resp.Body)
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

func handleRequest(w http.ResponseWriter, r *http.Request, config *Config, proxy *httputil.ReverseProxy) {
	if err := limiter.Wait(r.Context()); err != nil {
		http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	if config.GithubAppID != "" && len(config.GithubAppKey) > 0 {
		jwt, err := generateJWT(config.GithubAppID, config.GithubAppKey)
		if err != nil {
			http.Error(w, "Failed to generate JWT", http.StatusInternalServerError)
			return
		}

		token, err := getInstallationToken(jwt, config.InstallationID)
		if err != nil {
			http.Error(w, "Failed to get installation token", http.StatusInternalServerError)
			return
		}

		r.Header.Set("Authorization", "token "+token)
	} else if auth := r.Header.Get("Authorization"); auth != "" {
		r.Header.Set("Authorization", auth)
	}

	r.Host = "api.github.com"
	proxy.ServeHTTP(w, r)
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

func setupServer(config *Config) (*http.Server, error) {
	target, err := url.Parse(githubAPIURL)
	if err != nil {
		return nil, err
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ModifyResponse = modifyResponse

	handler := func(w http.ResponseWriter, r *http.Request) {
		handleRequest(w, r, config, proxy)
	}

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", config.Port),
		Handler: http.HandlerFunc(handler),
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}

	return server, nil
}

func main() {
	config, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}

	server, err := setupServer(config)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Starting kado-proxy HTTPS server on :%d\n", config.Port)
	log.Fatal(server.ListenAndServeTLS(config.CertFile, config.KeyFile))
}