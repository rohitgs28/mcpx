package auth

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/rohitgs28/mcpx/internal/config"
)

// OAuthValidator validates RS256 JWT access tokens for an MCP resource server.
//
// Per the MCP authorization spec it enforces two things a confused-deputy /
// token-passthrough attacker cannot satisfy: a valid signature from the
// configured authorization server (verified against its JWKS), and RFC 8707
// audience binding — the token's aud claim MUST contain this gateway's resource
// identifier. Tokens are validated, never forwarded upstream.
type OAuthValidator struct {
	cfg    config.OAuthConfig
	client *http.Client

	mu      sync.RWMutex
	keys    map[string]*rsa.PublicKey
	fetched time.Time
}

// NewOAuthValidator builds a validator. JWKS keys are fetched lazily on first
// use and refreshed when an unknown key id is seen (key rotation).
func NewOAuthValidator(cfg config.OAuthConfig) *OAuthValidator {
	return &OAuthValidator{
		cfg:    cfg,
		client: &http.Client{Timeout: 5 * time.Second},
		keys:   make(map[string]*rsa.PublicKey),
	}
}

// Validate verifies a bearer token's signature, expiry, issuer (if configured),
// and audience. On success it returns the client identity read from the
// identityClaim ("" when the claim is absent or not a string).
func (v *OAuthValidator) Validate(tokenStr, identityClaim string) (string, error) {
	opts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithAudience(v.cfg.Resource), // RFC 8707 audience binding
		jwt.WithExpirationRequired(),
	}
	if v.cfg.Issuer != "" {
		opts = append(opts, jwt.WithIssuer(v.cfg.Issuer))
	}
	tok, err := jwt.Parse(tokenStr, v.keyfunc, opts...)
	if err != nil {
		return "", err
	}
	if identityClaim == "" {
		identityClaim = "sub"
	}
	if claims, ok := tok.Claims.(jwt.MapClaims); ok {
		if id, ok := claims[identityClaim].(string); ok {
			return id, nil
		}
	}
	return "", nil
}

func (v *OAuthValidator) keyfunc(token *jwt.Token) (any, error) {
	kid, _ := token.Header["kid"].(string)
	if key := v.lookup(kid); key != nil {
		return key, nil
	}
	if err := v.refresh(); err != nil {
		return nil, fmt.Errorf("fetching JWKS: %w", err)
	}
	if key := v.lookup(kid); key != nil {
		return key, nil
	}
	return nil, fmt.Errorf("no matching key for kid %q", kid)
}

func (v *OAuthValidator) lookup(kid string) *rsa.PublicKey {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.keys[kid]
}

// refresh fetches and parses the JWKS, throttled to avoid hammering the
// endpoint when clients present tokens with unrecognized key ids.
func (v *OAuthValidator) refresh() error {
	v.mu.RLock()
	throttled := len(v.keys) > 0 && time.Since(v.fetched) < 10*time.Second
	v.mu.RUnlock()
	if throttled {
		return nil
	}

	resp, err := v.client.Get(v.cfg.JWKSURI)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS endpoint returned %d", resp.StatusCode)
	}
	var set struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return err
	}
	keys := make(map[string]*rsa.PublicKey)
	for _, k := range set.Keys {
		if k.Kty != "RSA" {
			continue
		}
		pk, err := rsaPublicKey(k.N, k.E)
		if err != nil {
			continue
		}
		keys[k.Kid] = pk
	}
	if len(keys) == 0 {
		return fmt.Errorf("JWKS contained no usable RSA keys")
	}
	v.mu.Lock()
	v.keys = keys
	v.fetched = time.Now()
	v.mu.Unlock()
	return nil
}

func rsaPublicKey(nB64, eB64 string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nB64)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eB64)
	if err != nil {
		return nil, err
	}
	e := new(big.Int).SetBytes(eBytes)
	if !e.IsInt64() || e.Int64() > 1<<31-1 {
		return nil, fmt.Errorf("invalid RSA exponent")
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: int(e.Int64())}, nil
}

// challenge builds the WWW-Authenticate value advertising RFC 9728 metadata
// discovery on a 401 response.
func challenge(resource string) string {
	return fmt.Sprintf(`Bearer resource_metadata="%s%s"`, strings.TrimRight(resource, "/"), MetadataPath)
}

// MetadataPath is the well-known location of the Protected Resource Metadata
// document (RFC 9728).
const MetadataPath = "/.well-known/oauth-protected-resource"

// ProtectedResourceMetadataHandler serves the RFC 9728 Protected Resource
// Metadata document so clients can discover which authorization servers issue
// tokens for this gateway.
func ProtectedResourceMetadataHandler(cfg config.OAuthConfig) http.HandlerFunc {
	doc := map[string]any{"resource": cfg.Resource}
	if len(cfg.AuthorizationServers) > 0 {
		doc["authorization_servers"] = cfg.AuthorizationServers
	}
	body, _ := json.Marshal(doc)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}
}
