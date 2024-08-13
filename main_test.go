package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"testing"
	"time"
	"flag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestLoadConfig(t *testing.T) {
	os.Setenv("GITHUB_APP_ID", "test-app-id")
	os.Setenv("GITHUB_APP_PRIVATE_KEY", "test-private-key")
	os.Setenv("GITHUB_INSTALLATION_ID", "12345")

	oldArgs := os.Args
	os.Args = []string{"cmd", "-cert=test.crt", "-key=test.key", "-port=8080"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError) // Reset flags
	config, err := loadConfig()
	os.Args = oldArgs

	assert.NoError(t, err)
	assert.Equal(t, "test-app-id", config.GithubAppID)
	assert.Equal(t, []byte("test-private-key"), config.GithubAppKey)
	assert.Equal(t, int64(12345), config.InstallationID)
	assert.Equal(t, "test.crt", config.CertFile)
	assert.Equal(t, "test.key", config.KeyFile)
	assert.Equal(t, 8080, config.Port)

	os.Args = []string{"cmd"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError) // Reset flags
	_, err = loadConfig()
	assert.Error(t, err)

	os.Args = oldArgs
}

func TestGenerateJWT(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	assert.NoError(t, err)

	pemdata := pem.EncodeToMemory(
		&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
		},
	)

	jwt, err := generateJWT("test-app-id", pemdata)
	assert.NoError(t, err)
	assert.NotEmpty(t, jwt)

	_, err = generateJWT("test-app-id", []byte("invalid-key"))
	assert.Error(t, err)
}

type mockHTTPClient struct {
	mock.Mock
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	args := m.Called(req)
	return args.Get(0).(*http.Response), args.Error(1)
}

func TestGetInstallationToken(t *testing.T) {
	mockClient := new(mockHTTPClient)

	mockResponse := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(`{"token": "test-token"}`)),
	}
	mockClient.On("Do", mock.Anything).Return(mockResponse, nil)

	token, err := getInstallationToken("test-jwt", 12345)
	assert.NoError(t, err)
	assert.Equal(t, "test-token", token)

	mockErrorResponse := &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(bytes.NewBufferString(`{"message": "Bad request"}`)),
	}
	mockClient.On("Do", mock.Anything).Return(mockErrorResponse, nil)

	_, err = getInstallationToken("test-jwt", 12345)
	assert.Error(t, err)
}

func TestHandleRequest(t *testing.T) {
	config := &Config{
		GithubAppID:    "test-app-id",
		GithubAppKey:   []byte("test-private-key"),
		InstallationID: 12345,
	}

	target, _ := url.Parse(githubAPIURL)
	proxy := httputil.NewSingleHostReverseProxy(target)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handleRequest(w, req, config, proxy)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, req.Header.Get("Authorization"), "token ")

	for i := 0; i < 10; i++ {
		w = httptest.NewRecorder()
		handleRequest(w, req, config, proxy)
	}

	assert.Equal(t, http.StatusTooManyRequests, w.Code)

	config.GithubAppID = ""
	config.GithubAppKey = nil
	req.Header.Set("Authorization", "Bearer test-token")
	w = httptest.NewRecorder()

	handleRequest(w, req, config, proxy)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Bearer test-token", req.Header.Get("Authorization"))
}

func TestModifyResponse(t *testing.T) {
	rateLimitResp := RateLimitResponse{}
	rateLimitResp.Resources.Core.Limit = 5000
	rateLimitResp.Resources.Core.Remaining = 0
	rateLimitResp.Resources.Core.Reset = int(time.Now().Add(time.Hour).Unix())

	body, _ := json.Marshal(rateLimitResp)

	resp := &http.Response{
		StatusCode: http.StatusForbidden,
		Body:       io.NopCloser(bytes.NewBuffer(body)),
	}

	err := modifyResponse(resp)

	assert.NoError(t, err)

	resp = &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString("OK")),
	}

	err = modifyResponse(resp)

	assert.NoError(t, err)
}

func TestSetupServer(t *testing.T) {
	config := &Config{
		GithubAppID:    "test-app-id",
		GithubAppKey:   []byte("test-private-key"),
		InstallationID: 12345,
		CertFile:       "test.crt",
		KeyFile:        "test.key",
		Port:           8443,
	}

	server, err := setupServer(config)

	assert.NoError(t, err)
	assert.NotNil(t, server)
	assert.Equal(t, ":8443", server.Addr)
}

func TestMain(t *testing.T) {
	os.Setenv("GITHUB_APP_ID", "test-app-id")
	os.Setenv("GITHUB_APP_PRIVATE_KEY", "test-private-key")
	os.Setenv("GITHUB_INSTALLATION_ID", "12345")

	oldArgs := os.Args
	os.Args = []string{"cmd", "-cert=test.crt", "-key=test.key", "-port=8443"}

	config, err := loadConfig()
	assert.NoError(t, err)
	
	server, err := setupServer(config)
	assert.NoError(t, err)
	assert.NotNil(t, server)

	os.Args = oldArgs
}

func generateTestCert() ([]byte, []byte, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test Co"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour * 24),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})

	return certPEM, keyPEM, nil
}