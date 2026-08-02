// Package auth also carries the external identity providers. This file holds
// the Google integration: building an authorization URL, exchanging the code,
// and normalising the result into models.ExternalIdentity.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/Karan0009/wordotron_api/app/config"
	"github.com/Karan0009/wordotron_api/app/models"
)

// Google's endpoints. These are stable and published; fetching the discovery
// document on every start-up would add a hard dependency on Google being
// reachable before this service can accept traffic.
const (
	googleAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL = "https://oauth2.googleapis.com/token"
)

// googleIssuers are both spellings Google uses in the `iss` claim.
var googleIssuers = []string{"https://accounts.google.com", "accounts.google.com"}

var (
	// ErrProviderDisabled is returned when Google sign-in is not configured.
	ErrProviderDisabled = errors.New("auth: google sign-in is not configured")
	// ErrExchangeFailed covers any failure talking to the provider.
	ErrExchangeFailed = errors.New("auth: could not complete the google exchange")
	// ErrIdentityUnusable means the provider returned a profile we cannot act
	// on, such as an unverified email address.
	ErrIdentityUnusable = errors.New("auth: google returned an unusable profile")
)

// IdentityProvider is the boundary the service layer depends on, so a second
// provider can be added without touching the sign-in use case.
type IdentityProvider interface {
	// AuthCodeURL builds the URL the browser is sent to.
	AuthCodeURL(state, codeChallenge string) string
	// Exchange turns an authorization code into a normalised identity.
	Exchange(ctx context.Context, code, codeVerifier string) (*models.ExternalIdentity, error)
	Provider() models.Provider
}

// GoogleProvider implements the authorization code flow with PKCE.
type GoogleProvider struct {
	clientID     string
	clientSecret string
	redirectURL  string
	client       *http.Client
	now          func() time.Time
}

var _ IdentityProvider = (*GoogleProvider)(nil)

// NewGoogleProvider builds the provider. It returns nil when Google sign-in is
// not configured, which the caller treats as "the feature is off" rather than
// an error, so the service still starts without Google credentials.
func NewGoogleProvider(cfg config.Google) *GoogleProvider {
	if !cfg.Enabled() {
		return nil
	}

	return &GoogleProvider{
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		redirectURL:  cfg.RedirectURL,
		client: &http.Client{
			// A hung provider must not hold a request goroutine open.
			Timeout: 10 * time.Second,
		},
		now: time.Now,
	}
}

// Provider identifies this implementation.
func (g *GoogleProvider) Provider() models.Provider { return models.ProviderGoogle }

// AuthCodeURL builds the consent URL.
func (g *GoogleProvider) AuthCodeURL(state, codeChallenge string) string {
	params := url.Values{
		"client_id":     {g.clientID},
		"redirect_uri":  {g.redirectURL},
		"response_type": {"code"},
		"scope":         {"openid email profile"},
		"state":         {state},
		// PKCE. The verifier never leaves this service, so an intercepted code
		// is useless without it.
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
		// No refresh token is requested: this service does not call Google APIs
		// on the person's behalf, it only establishes who they are.
		"access_type": {"online"},
		"prompt":      {"select_account"},
	}
	return googleAuthURL + "?" + params.Encode()
}

type googleTokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Error       string `json:"error"`
	ErrorDesc   string `json:"error_description"`
}

// googleClaims is the subset of the ID token this service uses.
type googleClaims struct {
	jwt.RegisteredClaims
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	GivenName     string `json:"given_name"`
	Picture       string `json:"picture"`
	HostedDomain  string `json:"hd"`
}

// Exchange trades the authorization code for an ID token and reads the profile
// out of it.
func (g *GoogleProvider) Exchange(ctx context.Context, code, codeVerifier string) (*models.ExternalIdentity, error) {
	form := url.Values{
		"code":          {code},
		"client_id":     {g.clientID},
		"client_secret": {g.clientSecret},
		"redirect_uri":  {g.redirectURL},
		"grant_type":    {"authorization_code"},
		"code_verifier": {codeVerifier},
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, googleTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrExchangeFailed, err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")

	response, err := g.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrExchangeFailed, err)
	}
	defer func() { _ = response.Body.Close() }()

	// Cap the read: a misbehaving or impersonated endpoint must not be able to
	// stream an unbounded body into memory.
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("%w: reading response: %v", ErrExchangeFailed, err)
	}

	var payload googleTokenResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("%w: malformed response", ErrExchangeFailed)
	}

	if response.StatusCode != http.StatusOK || payload.Error != "" {
		// The provider's error text is logged by the caller but never shown to
		// the person: it leaks configuration detail.
		return nil, fmt.Errorf("%w: %s %s", ErrExchangeFailed, payload.Error, payload.ErrorDesc)
	}
	if payload.IDToken == "" {
		return nil, fmt.Errorf("%w: no id_token in response", ErrExchangeFailed)
	}

	return g.identityFromIDToken(payload.IDToken)
}

// identityFromIDToken reads the claims and maps them into the domain shape.
//
// The signature is deliberately not verified. Per OpenID Connect Core 3.1.3.7,
// a token received directly from the token endpoint over a TLS connection that
// authenticated this client may be trusted without validating its signature —
// TLS already establishes that Google sent it. Fetching and caching JWKS would
// add a second network dependency for no additional guarantee on this path. The
// issuer, audience and expiry are still checked, because those catch
// misconfiguration rather than forgery.
func (g *GoogleProvider) identityFromIDToken(raw string) (*models.ExternalIdentity, error) {
	var claims googleClaims

	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	if _, _, err := parser.ParseUnverified(raw, &claims); err != nil {
		return nil, fmt.Errorf("%w: unreadable id_token", ErrExchangeFailed)
	}

	issuerOK := false
	for _, issuer := range googleIssuers {
		if claims.Issuer == issuer {
			issuerOK = true
			break
		}
	}
	if !issuerOK {
		return nil, fmt.Errorf("%w: unexpected issuer %q", ErrExchangeFailed, claims.Issuer)
	}

	if !claims.VerifyAudience(g.clientID) {
		return nil, fmt.Errorf("%w: token was issued for another client", ErrExchangeFailed)
	}

	if claims.ExpiresAt == nil || claims.ExpiresAt.Before(g.now()) {
		return nil, fmt.Errorf("%w: id_token has expired", ErrExchangeFailed)
	}

	if claims.Subject == "" {
		return nil, fmt.Errorf("%w: id_token has no subject", ErrExchangeFailed)
	}

	email := strings.ToLower(strings.TrimSpace(claims.Email))
	if email == "" {
		return nil, fmt.Errorf("%w: no email address", ErrIdentityUnusable)
	}

	// An unverified address must never be used to find or link a local account:
	// that would let anyone claim someone else's email at the provider and take
	// over the matching account here.
	if !claims.EmailVerified {
		return nil, fmt.Errorf("%w: email address is not verified with Google", ErrIdentityUnusable)
	}

	fullName := strings.TrimSpace(claims.Name)
	if fullName == "" {
		fullName = strings.TrimSpace(claims.GivenName)
	}
	if fullName == "" {
		// Something has to go in the required column; the local part of the
		// address is the least surprising placeholder.
		fullName = strings.SplitN(email, "@", 2)[0]
	}

	return &models.ExternalIdentity{
		Provider:      models.ProviderGoogle,
		Subject:       claims.Subject,
		Email:         email,
		EmailVerified: true,
		FullName:      fullName,
		AvatarURL:     claims.Picture,
	}, nil
}

// verifyAudience is a small helper because jwt.RegisteredClaims stores the
// audience as a list.
func (c googleClaims) VerifyAudience(expected string) bool {
	for _, audience := range c.Audience {
		if audience == expected {
			return true
		}
	}
	return false
}

/* ---------------------------------------------------------------------------
 * PKCE
 * ------------------------------------------------------------------------ */

// NewCodeVerifier returns a high-entropy PKCE verifier (RFC 7636 §4.1).
func NewCodeVerifier() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate code verifier: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// CodeChallenge derives the S256 challenge from a verifier (RFC 7636 §4.2).
func CodeChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// NewOpaqueToken returns a URL-safe random string, used for the `state`
// parameter and for the one-time hand-off code.
func NewOpaqueToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
