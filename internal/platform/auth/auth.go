package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/FootGrid/footgrid/internal/platform/httpapi"
	"github.com/golang-jwt/jwt/v5"
)

const maxTokenBytes = 64 << 10

type Principal struct {
	Subject string
	Claims  jwt.MapClaims
}

type contextKey struct{}

type TokenVerifier interface {
	Verify(ctx context.Context, token string) (Principal, error)
}

func FromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(contextKey{}).(Principal)
	return principal, ok
}

func Middleware(verifier TokenVerifier, next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/health" {
			next.ServeHTTP(writer, request)
			return
		}
		if verifier == nil {
			httpapi.WriteProblem(writer, http.StatusServiceUnavailable, "auth-unavailable", "authentication verifier is not configured", request)
			return
		}
		header := strings.TrimSpace(request.Header.Get("Authorization"))
		if len(header) < 8 || !strings.EqualFold(header[:7], "Bearer ") {
			httpapi.WriteProblem(writer, http.StatusUnauthorized, "unauthorized", "Bearer authentication is required", request)
			return
		}
		principal, err := verifier.Verify(request.Context(), strings.TrimSpace(header[7:]))
		if err != nil {
			httpapi.WriteProblem(writer, http.StatusUnauthorized, "unauthorized", "invalid bearer token", request)
			return
		}
		next.ServeHTTP(writer, request.WithContext(context.WithValue(request.Context(), contextKey{}, principal)))
	})
}

type JWTVerifier struct {
	issuer   string
	audience string
	jwksURL  string
	client   *http.Client
	mu       sync.RWMutex
	keys     map[string]*rsa.PublicKey
	expires  time.Time
}

func NewJWTVerifier(issuer, audience string) (*JWTVerifier, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(issuer), "/"))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, errors.New("Cognito issuer must be an HTTPS URL")
	}
	return &JWTVerifier{issuer: strings.TrimRight(issuer, "/"), audience: audience, jwksURL: strings.TrimRight(issuer, "/") + "/.well-known/jwks.json", client: &http.Client{Timeout: 5 * time.Second}, keys: make(map[string]*rsa.PublicKey)}, nil
}

func (verifier *JWTVerifier) Verify(ctx context.Context, tokenValue string) (Principal, error) {
	if tokenValue == "" || len(tokenValue) > maxTokenBytes {
		return Principal{}, errors.New("token size is invalid")
	}
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(tokenValue, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodRS256 {
			return nil, errors.New("unexpected signing method")
		}
		keyID, ok := token.Header["kid"].(string)
		if !ok || keyID == "" {
			return nil, errors.New("token key ID is missing")
		}
		return verifier.publicKey(ctx, keyID)
	})
	if err != nil || !token.Valid {
		return Principal{}, errors.New("token signature is invalid")
	}
	issuer, err := claims.GetIssuer()
	if err != nil || issuer != verifier.issuer {
		return Principal{}, errors.New("token issuer is invalid")
	}
	audience, err := claims.GetAudience()
	if err != nil || !contains(audience, verifier.audience) {
		return Principal{}, errors.New("token audience is invalid")
	}
	subject, err := claims.GetSubject()
	if err != nil || strings.TrimSpace(subject) == "" {
		return Principal{}, errors.New("token subject is missing")
	}
	return Principal{Subject: subject, Claims: claims}, nil
}

func (verifier *JWTVerifier) publicKey(ctx context.Context, keyID string) (*rsa.PublicKey, error) {
	verifier.mu.RLock()
	key, valid := verifier.keys[keyID], time.Now().Before(verifier.expires)
	verifier.mu.RUnlock()
	if valid {
		return key, nil
	}
	if err := verifier.refresh(ctx); err != nil {
		return nil, err
	}
	verifier.mu.RLock()
	defer verifier.mu.RUnlock()
	key, ok := verifier.keys[keyID]
	if !ok {
		return nil, errors.New("token key is unknown")
	}
	return key, nil
}

func (verifier *JWTVerifier) refresh(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, verifier.jwksURL, nil)
	if err != nil {
		return err
	}
	response, err := verifier.client.Do(request)
	if err != nil {
		return fmt.Errorf("fetch Cognito keys: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("Cognito keys returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	var document struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := json.Unmarshal(body, &document); err != nil {
		return err
	}
	keys := make(map[string]*rsa.PublicKey, len(document.Keys))
	for _, raw := range document.Keys {
		keyID, key, err := parseRSAKey(raw)
		if err == nil {
			keys[keyID] = key
		}
	}
	if len(keys) == 0 {
		return errors.New("Cognito JWKS contains no usable RSA keys")
	}
	verifier.mu.Lock()
	verifier.keys, verifier.expires = keys, time.Now().Add(5*time.Minute)
	verifier.mu.Unlock()
	return nil
}

func parseRSAKey(raw []byte) (string, *rsa.PublicKey, error) {
	var value struct {
		KID string `json:"kid"`
		KTY string `json:"kty"`
		N   string `json:"n"`
		E   string `json:"e"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", nil, err
	}
	if value.KTY != "RSA" || value.KID == "" {
		return "", nil, errors.New("invalid RSA JWK")
	}
	modulus, err := base64.RawURLEncoding.DecodeString(value.N)
	if err != nil {
		return "", nil, err
	}
	exponent, err := base64.RawURLEncoding.DecodeString(value.E)
	if err != nil {
		return "", nil, err
	}
	if len(modulus) == 0 || len(exponent) == 0 {
		return "", nil, errors.New("invalid RSA JWK numbers")
	}
	var exponentValue int
	for _, byteValue := range exponent {
		exponentValue = exponentValue<<8 | int(byteValue)
	}
	if exponentValue < 2 {
		return "", nil, errors.New("invalid RSA exponent")
	}
	return value.KID, &rsa.PublicKey{N: new(big.Int).SetBytes(modulus), E: exponentValue}, nil
}

func contains(values jwt.ClaimStrings, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
