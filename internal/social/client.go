package social

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Identity is who the provider says signed in.
type Identity struct {
	// Subject is the provider's stable id for the account.
	Subject string
	Email   string
	// EmailVerified: the provider confirmed the person owns Email.
	EmailVerified bool
	// Name identifies the account at the provider (a GitHub login, an email).
	Name string
	// Nickname is a suggestion for a new account.
	Nickname string
}

// endpoints of one provider.
type endpoints struct {
	issuer   string // oidc: the expected iss; microsoft: built from the tid claim
	auth     string
	token    string
	userinfo string
	// basicAuth: send the client secret with HTTP Basic, not in the form.
	basicAuth bool
}

// Microsoft's tenant for personal accounts: their email addresses are the verified
// sign-in names. Work and school accounts' email claims are set by their
// organization's administrators and prove nothing (the "nOAuth" takeover).
const microsoftConsumers = "9188040d-6c67-4c5b-b112-36a304b66dad"

const (
	maxResponse  = 1 << 20
	discoveryTTL = time.Hour
	clockSkew    = 5 * time.Minute
)

// client talks to providers.
type client struct {
	http *http.Client
	now  func() time.Time
	// Where GitHub and Microsoft live; tests point them at fakes.
	githubWeb, githubAPI, microsoftLogin string

	mu        sync.Mutex
	discovery map[string]cachedDiscovery
}

type cachedDiscovery struct {
	at  time.Time
	eps endpoints
}

func newClient() *client {
	return &client{
		// Providers are reached directly: a redirect is never part of these calls.
		http: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
		now:            time.Now,
		githubWeb:      "https://github.com",
		githubAPI:      "https://api.github.com",
		microsoftLogin: "https://login.microsoftonline.com",
		discovery:      map[string]cachedDiscovery{},
	}
}

func (c *client) endpoints(ctx context.Context, p ProviderConfig) (endpoints, error) {
	switch p.Type {
	case TypeGitHub:
		return endpoints{auth: c.githubWeb + "/login/oauth/authorize", token: c.githubWeb + "/login/oauth/access_token",
			userinfo: c.githubAPI + "/user"}, nil
	case TypeMicrosoft:
		base := c.microsoftLogin + "/" + url.PathEscape(microsoftTenant(p)) + "/oauth2/v2.0"
		return endpoints{auth: base + "/authorize", token: base + "/token"}, nil
	}
	return c.discover(ctx, p.Issuer)
}

func microsoftTenant(p ProviderConfig) string {
	if p.Tenant == "" {
		return "common"
	}
	return p.Tenant
}

// discover reads an OpenID Connect provider's metadata, cached for an hour.
func (c *client) discover(ctx context.Context, issuer string) (endpoints, error) {
	c.mu.Lock()
	cached, ok := c.discovery[issuer]
	c.mu.Unlock()
	if ok && c.now().Sub(cached.at) < discoveryTTL {
		return cached.eps, nil
	}
	var doc struct {
		Issuer      string   `json:"issuer"`
		Auth        string   `json:"authorization_endpoint"`
		Token       string   `json:"token_endpoint"`
		Userinfo    string   `json:"userinfo_endpoint"`
		AuthMethods []string `json:"token_endpoint_auth_methods_supported"`
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(issuer, "/")+"/.well-known/openid-configuration", nil)
	if err != nil {
		return endpoints{}, err
	}
	if err := c.do(req, &doc); err != nil {
		return endpoints{}, fmt.Errorf("discovery: %w", err)
	}
	// OpenID Connect Discovery §4.3: the document must be the issuer's own.
	if doc.Issuer != issuer {
		return endpoints{}, fmt.Errorf("discovery: issuer %q, want %q", doc.Issuer, issuer)
	}
	for _, e := range []string{doc.Auth, doc.Token} {
		if err := checkEndpoint(e); err != nil {
			return endpoints{}, fmt.Errorf("discovery: endpoint %q %w", e, err)
		}
	}
	if doc.Userinfo != "" && checkEndpoint(doc.Userinfo) != nil {
		doc.Userinfo = ""
	}
	eps := endpoints{issuer: doc.Issuer, auth: doc.Auth, token: doc.Token, userinfo: doc.Userinfo,
		// The default is client_secret_basic (OpenID Connect Discovery §3).
		basicAuth: !slices.Contains(doc.AuthMethods, "client_secret_post")}
	c.mu.Lock()
	c.discovery[issuer] = cachedDiscovery{at: c.now(), eps: eps}
	c.mu.Unlock()
	return eps, nil
}

// authURL is where the browser goes to sign in.
func (c *client) authURL(p ProviderConfig, eps endpoints, redirectURI, state, challenge, nonce string) string {
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {p.ClientID},
		"redirect_uri":          {redirectURI},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	switch p.Type {
	case TypeGitHub:
		q.Set("scope", "read:user user:email")
	case TypeMicrosoft:
		q.Set("scope", "openid profile email")
		q.Set("nonce", nonce)
		q.Set("prompt", "select_account")
	default:
		scopes := []string{"openid", "email", "profile"}
		for _, s := range p.Scopes {
			if !slices.Contains(scopes, s) {
				scopes = append(scopes, s)
			}
		}
		q.Set("scope", strings.Join(scopes, " "))
		q.Set("nonce", nonce)
	}
	sep := "?"
	if strings.Contains(eps.auth, "?") {
		sep = "&"
	}
	return eps.auth + sep + q.Encode()
}

// identity exchanges the code and finds out who signed in.
func (c *client) identity(ctx context.Context, p ProviderConfig, eps endpoints, redirectURI, code, verifier, nonce string) (*Identity, error) {
	tok, err := c.exchange(ctx, p, eps, redirectURI, code, verifier)
	if err != nil {
		return nil, err
	}
	if p.Type == TypeGitHub {
		return c.github(ctx, tok.AccessToken)
	}
	claims, err := c.idToken(p, eps, tok.IDToken, nonce)
	if err != nil {
		return nil, err
	}
	if claims.Email == "" && eps.userinfo != "" {
		if err := c.userinfo(ctx, eps.userinfo, tok.AccessToken, claims); err != nil {
			return nil, err
		}
	}
	id := &Identity{Subject: claims.Subject, Email: claims.Email}
	if p.Type == TypeMicrosoft {
		id.EmailVerified = claims.Email != "" && (claims.TenantID == microsoftConsumers || claims.EmailDomainVerified)
	} else {
		id.EmailVerified = claims.Email != "" && bool(claims.EmailVerified)
	}
	id.Name = firstOf(claims.PreferredUsername, claims.Email, claims.Subject)
	id.Nickname = firstOf(claims.Name, claims.Nickname, claims.PreferredUsername, localPart(claims.Email))
	return id, nil
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

func (c *client) exchange(ctx context.Context, p ProviderConfig, eps endpoints, redirectURI, code, verifier string) (*tokenResponse, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}
	basic := eps.basicAuth && p.ClientSecret != ""
	if !basic {
		form.Set("client_id", p.ClientID)
		if p.ClientSecret != "" {
			form.Set("client_secret", p.ClientSecret)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, eps.token, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if basic {
		// RFC 6749 §2.3.1: both parts are form-encoded first.
		req.SetBasicAuth(url.QueryEscape(p.ClientID), url.QueryEscape(p.ClientSecret))
	}
	var tok tokenResponse
	err = c.do(req, &tok)
	if tok.Error != "" { // GitHub answers errors with 200
		return nil, fmt.Errorf("token: %s: %s", tok.Error, tok.Description)
	}
	if err != nil {
		return nil, fmt.Errorf("token: %w", err)
	}
	if tok.AccessToken == "" || (p.Type != TypeGitHub && tok.IDToken == "") {
		return nil, errors.New("token: the response has no access_token or id_token")
	}
	return &tok, nil
}

// lenientBool is a boolean claim some providers send as a string ("true").
type lenientBool bool

func (b *lenientBool) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	*b = lenientBool(s == "true")
	return nil
}

type audience []string

func (a *audience) UnmarshalJSON(data []byte) error {
	var one string
	if json.Unmarshal(data, &one) == nil {
		*a = audience{one}
		return nil
	}
	return json.Unmarshal(data, (*[]string)(a))
}

type idClaims struct {
	Issuer              string      `json:"iss"`
	Subject             string      `json:"sub"`
	Audience            audience    `json:"aud"`
	AuthorizedParty     string      `json:"azp"`
	Expires             json.Number `json:"exp"`
	Nonce               string      `json:"nonce"`
	Email               string      `json:"email"`
	EmailVerified       lenientBool `json:"email_verified"`
	Name                string      `json:"name"`
	Nickname            string      `json:"nickname"`
	PreferredUsername   string      `json:"preferred_username"`
	TenantID            string      `json:"tid"`
	EmailDomainVerified bool        `json:"xms_edov"`
}

// idToken checks the ID token's claims. Its signature is not checked: the token came
// straight from the provider's token endpoint over TLS, which OpenID Connect Core
// §3.1.3.7 (6) accepts in place of the signature.
func (c *client) idToken(p ProviderConfig, eps endpoints, token, nonce string) (*idClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("id_token: not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return nil, errors.New("id_token: bad encoding")
	}
	var claims idClaims
	dec := json.NewDecoder(strings.NewReader(string(payload)))
	dec.UseNumber()
	if err := dec.Decode(&claims); err != nil {
		return nil, fmt.Errorf("id_token: %w", err)
	}
	want := eps.issuer
	if p.Type == TypeMicrosoft {
		if err := checkMicrosoftTenant(microsoftTenant(p), claims.TenantID); err != nil {
			return nil, err
		}
		want = c.microsoftLogin + "/" + claims.TenantID + "/v2.0"
	}
	exp, err := claims.Expires.Int64()
	switch {
	case claims.Issuer != want:
		return nil, fmt.Errorf("id_token: issuer %q, want %q", claims.Issuer, want)
	case !slices.Contains(claims.Audience, p.ClientID) || (len(claims.Audience) > 1 && claims.AuthorizedParty != p.ClientID):
		return nil, errors.New("id_token: issued to another client")
	case err != nil || c.now().After(time.Unix(exp, 0).Add(clockSkew)):
		return nil, errors.New("id_token: expired")
	case claims.Nonce != nonce:
		return nil, errors.New("id_token: nonce mismatch")
	case claims.Subject == "" || len(claims.Subject) > 255:
		return nil, errors.New("id_token: bad subject")
	}
	return &claims, nil
}

func checkMicrosoftTenant(tenant, tid string) error {
	if tid == "" {
		return errors.New("id_token: no tenant")
	}
	switch strings.ToLower(tenant) {
	case "common":
		return nil
	case "consumers":
		if tid == microsoftConsumers {
			return nil
		}
	case "organizations":
		if tid != microsoftConsumers {
			return nil
		}
	default:
		// A tenant id must match; a domain name already limited the sign-in page and
		// the token endpoint to that tenant.
		if !looksLikeGUID(tenant) || strings.EqualFold(tenant, tid) {
			return nil
		}
	}
	return fmt.Errorf("id_token: tenant %s may not sign in", tid)
}

func looksLikeGUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return false
			}
		} else if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

// userinfo fills in the email when the ID token left it out.
func (c *client) userinfo(ctx context.Context, endpoint, accessToken string, claims *idClaims) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	var info idClaims
	if err := c.do(req, &info); err != nil {
		return fmt.Errorf("userinfo: %w", err)
	}
	if info.Subject != claims.Subject { // OpenID Connect Core §5.3.2
		return errors.New("userinfo: subject mismatch")
	}
	claims.Email, claims.EmailVerified = info.Email, info.EmailVerified
	claims.Name = firstOf(claims.Name, info.Name)
	claims.PreferredUsername = firstOf(claims.PreferredUsername, info.PreferredUsername)
	return nil
}

// github reads the GitHub account and its primary verified email address (the public
// profile email is not necessarily verified).
func (c *client) github(ctx context.Context, accessToken string) (*Identity, error) {
	get := func(path string, out any) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.githubAPI+path, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		return c.do(req, out)
	}
	var u struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := get("/user", &u); err != nil {
		return nil, fmt.Errorf("github user: %w", err)
	}
	if u.ID <= 0 {
		return nil, errors.New("github user: no id")
	}
	id := &Identity{Subject: strconv.FormatInt(u.ID, 10), Name: u.Login, Nickname: firstOf(u.Name, u.Login), Email: u.Email}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := get("/user/emails", &emails); err != nil {
		return id, nil // user:email was not granted: only the public address, unverified
	}
	for _, e := range emails {
		if e.Primary && e.Verified {
			id.Email, id.EmailVerified = e.Email, true
		}
	}
	return id, nil
}

// do sends req and decodes a JSON answer.
func (c *client) do(req *http.Request, out any) error {
	req.Header.Set("User-Agent", "SkinLoom")
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return err
	}
	decodeErr := json.Unmarshal(body, out)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s answered %d: %s", req.URL.Host, resp.StatusCode, truncate(string(body), 200))
	}
	return decodeErr
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

func localPart(email string) string {
	at := strings.LastIndexByte(email, '@')
	if at < 0 {
		return ""
	}
	return email[:at]
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
