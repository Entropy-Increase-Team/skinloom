package httpapi

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/oauth"
)

func verifyRS256JWT(t *testing.T, jwt string, jwk map[string]any) map[string]any {
	t.Helper()
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("malformed JWT: %q", jwt)
	}
	nBytes, err := base64.RawURLEncoding.DecodeString(jwk["n"].(string))
	if err != nil {
		t.Fatalf("decode jwk n: %v", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(jwk["e"].(string))
	if err != nil {
		t.Fatalf("decode jwk e: %v", err)
	}
	pub := &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: int(new(big.Int).SetBytes(eBytes).Int64()),
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decode jwt sig: %v", err)
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig); err != nil {
		t.Fatalf("RS256 verify failed: %v", err)
	}
	rawClaims, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(rawClaims, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	return claims
}

func TestOIDCAndYggdrasilConnectEndToEnd(t *testing.T) {
	d := newTestDeps(t)
	setSettings(t, d, `{"captcha.provider":"none","site.url":"https://skins.example.com","oauth.enable_oidc":true,"oauth.enable_dcr":true}`)
	h, _ := New(d)
	setupSite(t, d, &browser{t: t, h: h})
	alice := registerUser(t, h, "alice@example.com")

	var pResp PlayerCreated
	if rec := alice.do("POST", "/api/v1/me/players", `{"name":"AliceHero"}`); rec.Code != http.StatusOK {
		t.Fatalf("create player: %d %s", rec.Code, rec.Body)
	} else {
		_ = json.Unmarshal(rec.Body.Bytes(), &pResp)
	}

	// 1. Discovery + JWKS + Yggdrasil root meta advertisement.
	rec := launcher(t, h, "GET", "/.well-known/openid-configuration", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("openid-configuration: %d %s", rec.Code, rec.Body)
	}
	var disc map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &disc)
	if disc["issuer"] != "https://skins.example.com" ||
		disc["jwks_uri"] != "https://skins.example.com/oauth/jwks" ||
		disc["userinfo_endpoint"] != "https://skins.example.com/oauth/userinfo" ||
		disc["device_authorization_endpoint"] != "https://skins.example.com/oauth/device_authorization" ||
		disc["registration_endpoint"] != "https://skins.example.com/oauth/register" {
		t.Fatalf("unexpected discovery doc: %#v", disc)
	}

	jwksRec := launcher(t, h, "GET", "/oauth/jwks", "")
	if jwksRec.Code != http.StatusOK {
		t.Fatalf("jwks: %d %s", jwksRec.Code, jwksRec.Body)
	}
	var jwks struct {
		Keys []map[string]any `json:"keys"`
	}
	_ = json.Unmarshal(jwksRec.Body.Bytes(), &jwks)
	if len(jwks.Keys) != 1 || jwks.Keys[0]["alg"] != "RS256" {
		t.Fatalf("unexpected jwks: %#v", jwks)
	}

	// 2. RFC 7591 restricted DCR: rejects web https/non-loopback and confidential auth, accepts loopback & native scheme.
	if r := launcher(t, h, "POST", "/oauth/register", `{"client_name":"BadWeb","redirect_uris":["https://evil.example/cb"]}`); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on https DCR redirect, got %d %s", r.Code, r.Body)
	}
	if r := launcher(t, h, "POST", "/oauth/register", `{"client_name":"BadConf","redirect_uris":["http://127.0.0.1/cb"],"token_endpoint_auth_method":"client_secret_basic"}`); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on confidential DCR, got %d %s", r.Code, r.Body)
	}
	dcrRec := launcher(t, h, "POST", "/oauth/register", `{"client_name":"HMCL Launcher","redirect_uris":["http://127.0.0.1/cb","hmcl://callback"],"token_endpoint_auth_method":"none"}`)
	if dcrRec.Code != http.StatusCreated {
		t.Fatalf("DCR failed: %d %s", dcrRec.Code, dcrRec.Body)
	}
	var dcr struct {
		ClientID string `json:"client_id"`
	}
	_ = json.Unmarshal(dcrRec.Body.Bytes(), &dcr)
	if dcr.ClientID == "" {
		t.Fatalf("missing client_id in DCR response: %s", dcrRec.Body)
	}

	// 3. Scope validation runs AFTER client/redirect check (no open redirect on unknown client/redirect).
	badScopeRec := alice.do("GET", "/api/v1/oauth/authorize?response_type=code&client_id="+dcr.ClientID+"&redirect_uri="+url.QueryEscape("https://evil.example/cb")+"&scope="+url.QueryEscape("Yggdrasil.Server.Join"), "")
	var badInfo AuthorizeInfo
	_ = json.Unmarshal(badScopeRec.Body.Bytes(), &badInfo)
	if badInfo.Error == "" || badInfo.Redirect != "" {
		t.Fatalf("expected non-redirect RequestError on unregistered redirect_uri, got %+v", badInfo)
	}

	// Valid loopback dynamic port (RFC 8252) with invalid scope combo returns redirect error.
	dynRedirect := "http://127.0.0.1:54321/cb"
	verifier := "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
	chalSum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(chalSum[:])
	comboRec := alice.do("GET", "/api/v1/oauth/authorize?response_type=code&client_id="+dcr.ClientID+
		"&redirect_uri="+url.QueryEscape(dynRedirect)+"&scope="+url.QueryEscape("openid Yggdrasil.Server.Join")+
		"&code_challenge="+challenge+"&code_challenge_method=S256", "")
	var comboInfo AuthorizeInfo
	_ = json.Unmarshal(comboRec.Body.Bytes(), &comboInfo)
	if !strings.Contains(comboInfo.Redirect, "error=invalid_scope") {
		t.Fatalf("expected redirect error=invalid_scope for Join without Select, got %+v", comboInfo)
	}

	// 4. Full OIDC + Yggdrasil Connect AuthCode + PKCE + Nonce + Player Selection.
	scopeStr := "openid profile email offline_access Yggdrasil.PlayerProfiles.Select Yggdrasil.Server.Join"
	prepRec := alice.do("GET", "/api/v1/oauth/authorize?response_type=code&client_id="+dcr.ClientID+
		"&redirect_uri="+url.QueryEscape(dynRedirect)+"&scope="+url.QueryEscape(scopeStr)+
		"&state=st1&nonce=n-999&code_challenge="+challenge+"&code_challenge_method=S256", "")
	var prep AuthorizeInfo
	_ = json.Unmarshal(prepRec.Body.Bytes(), &prep)
	if !prep.NeedsPlayerSelect || len(prep.Players) != 1 || prep.Players[0].ID != pResp.Player.ID || prep.Granted {
		t.Fatalf("unexpected Prepare result: %+v", prep)
	}

	approveBody, _ := json.Marshal(map[string]any{
		"response_type":         "code",
		"client_id":             dcr.ClientID,
		"redirect_uri":          dynRedirect,
		"scope":                 scopeStr,
		"state":                 "st1",
		"nonce":                 "n-999",
		"code_challenge":        challenge,
		"code_challenge_method": "S256",
		"playerId":              pResp.Player.ID,
		"allow":                 true,
	})
	appRec := alice.do("POST", "/api/v1/oauth/authorize", string(approveBody))
	if appRec.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", appRec.Code, appRec.Body)
	}
	var appRes AuthorizeResult
	_ = json.Unmarshal(appRec.Body.Bytes(), &appRes)
	cbURL, _ := url.Parse(appRes.Redirect)
	code := cbURL.Query().Get("code")
	if code == "" || cbURL.Query().Get("state") != "st1" {
		t.Fatalf("unexpected callback URL: %s", appRes.Redirect)
	}

	tokForm := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {dcr.ClientID},
		"code":          {code},
		"redirect_uri":  {dynRedirect},
		"code_verifier": {verifier},
	}
	tokReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokForm.Encode()))
	tokReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokRec := httptest.NewRecorder()
	h.ServeHTTP(tokRec, tokReq)
	if tokRec.Code != http.StatusOK {
		t.Fatalf("token exchange: %d %s", tokRec.Code, tokRec.Body)
	}
	var tok oauth.TokenResponse
	_ = json.Unmarshal(tokRec.Body.Bytes(), &tok)
	if !strings.HasPrefix(tok.AccessToken, "slo_") || tok.RefreshToken == "" || tok.IDToken == "" {
		t.Fatalf("unexpected token response: %+v", tok)
	}

	// Verify RS256 id_token claims against /oauth/jwks.
	claims := verifyRS256JWT(t, tok.IDToken, jwks.Keys[0])
	if claims["iss"] != "https://skins.example.com" || claims["aud"] != dcr.ClientID || claims["nonce"] != "n-999" {
		t.Fatalf("unexpected id_token claims: %#v", claims)
	}

	// 5. UserInfo returns real email_verified=false before verification and selectedProfile.
	uiRec := launcher(t, h, "GET", "/oauth/userinfo", "", "Authorization", "Bearer "+tok.AccessToken)
	if uiRec.Code != http.StatusOK {
		t.Fatalf("userinfo: %d %s", uiRec.Code, uiRec.Body)
	}
	var ui map[string]any
	_ = json.Unmarshal(uiRec.Body.Bytes(), &ui)
	if ui["email"] != "alice@example.com" || ui["email_verified"] != false {
		t.Fatalf("expected real email_verified=false, got %#v", ui)
	}
	selProf := ui["selectedProfile"].(map[string]any)
	if selProf["id"] != pResp.Player.UUID || selProf["name"] != "AliceHero" {
		t.Fatalf("unexpected selectedProfile: %#v", selProf)
	}

	// Verify email -> UserInfo now reports email_verified=true.
	d.DB.Ent.User.Update().Where(user.Email("alice@example.com")).SetEmailVerifiedAt(time.Now()).ExecX(context.Background())
	uiRec2 := launcher(t, h, "GET", "/oauth/userinfo", "", "Authorization", "Bearer "+tok.AccessToken)
	_ = json.Unmarshal(uiRec2.Body.Bytes(), &ui)
	if ui["email_verified"] != true {
		t.Fatalf("expected email_verified=true after verification, got %#v", ui)
	}

	// 6. OIDC access token works natively on Yggdrasil sessionserver/join, hasJoined, validate, and fails legacy /authserver/refresh.
	if r := launcher(t, h, "POST", YggdrasilPrefix+"/authserver/refresh", fmt.Sprintf(`{"accessToken":%q}`, tok.AccessToken)); r.Code != http.StatusForbidden {
		t.Fatalf("OIDC token must not refresh on legacy /authserver/refresh, got %d %s", r.Code, r.Body)
	}
	if r := launcher(t, h, "POST", YggdrasilPrefix+"/sessionserver/session/minecraft/join",
		fmt.Sprintf(`{"accessToken":%q,"selectedProfile":%q,"serverId":"oidc-srv-1"}`, tok.AccessToken, pResp.Player.UUID)); r.Code != http.StatusNoContent {
		t.Fatalf("sessionserver/join with OIDC token: %d %s", r.Code, r.Body)
	}
	if r := launcher(t, h, "GET", YggdrasilPrefix+"/sessionserver/session/minecraft/hasJoined?username=AliceHero&serverId=oidc-srv-1", ""); r.Code != http.StatusOK || !strings.Contains(r.Body.String(), `"name":"AliceHero"`) {
		t.Fatalf("hasJoined after OIDC join: %d %s", r.Code, r.Body)
	}

	// 7. #231 rename invalidation: renaming bound player invalidates old access token until refreshed via /oauth/token.
	if r := alice.do("PATCH", fmt.Sprintf("/api/v1/me/players/%d", pResp.Player.ID), `{"name":"AliceRenamed"}`); r.Code != http.StatusOK {
		t.Fatalf("rename player: %d %s", r.Code, r.Body)
	}
	if r := launcher(t, h, "GET", "/oauth/userinfo", "", "Authorization", "Bearer "+tok.AccessToken); r.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on userinfo after player rename (#231), got %d %s", r.Code, r.Body)
	}
	if r := launcher(t, h, "POST", YggdrasilPrefix+"/sessionserver/session/minecraft/join",
		fmt.Sprintf(`{"accessToken":%q,"selectedProfile":%q,"serverId":"oidc-srv-2"}`, tok.AccessToken, pResp.Player.UUID)); r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on join after player rename (#231), got %d %s", r.Code, r.Body)
	}
	refForm := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {dcr.ClientID},
		"refresh_token": {tok.RefreshToken},
	}
	refReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(refForm.Encode()))
	refReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	refRec := httptest.NewRecorder()
	h.ServeHTTP(refRec, refReq)
	if refRec.Code != http.StatusOK {
		t.Fatalf("refresh after rename: %d %s", refRec.Code, refRec.Body)
	}
	var refTok oauth.TokenResponse
	_ = json.Unmarshal(refRec.Body.Bytes(), &refTok)
	uiRec3 := launcher(t, h, "GET", "/oauth/userinfo", "", "Authorization", "Bearer "+refTok.AccessToken)
	if uiRec3.Code != http.StatusOK || !strings.Contains(uiRec3.Body.String(), `"name":"AliceRenamed"`) {
		t.Fatalf("userinfo after refresh: %d %s", uiRec3.Code, uiRec3.Body)
	}

	// 8. Sign-out launchers (/api/v1/me/yggdrasil/signout) counts & revokes Yggdrasil Connect tokens.
	var yggCnt YggdrasilTokens
	_ = json.Unmarshal(alice.do("GET", "/api/v1/me/yggdrasil", "").Body.Bytes(), &yggCnt)
	if yggCnt.Tokens != 1 {
		t.Fatalf("expected 1 launcher token (OIDC Yggdrasil.Server.Join), got %d", yggCnt.Tokens)
	}
	if r := alice.do("POST", "/api/v1/me/yggdrasil/signout", ""); r.Code != http.StatusOK {
		t.Fatalf("signout launchers: %d %s", r.Code, r.Body)
	}
	if r := launcher(t, h, "GET", "/oauth/userinfo", "", "Authorization", "Bearer "+refTok.AccessToken); r.Code != http.StatusUnauthorized {
		t.Fatalf("expected OIDC launcher token revoked after signout, got %d", r.Code)
	}

	// 9. RFC 8628 Device Authorization Grant end-to-end.
	devForm := url.Values{
		"client_id": {dcr.ClientID},
		"scope":     {"openid profile offline_access Yggdrasil.PlayerProfiles.Select Yggdrasil.Server.Join"},
	}
	devReq := httptest.NewRequest(http.MethodPost, "/oauth/device_authorization", strings.NewReader(devForm.Encode()))
	devReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	devRec := httptest.NewRecorder()
	h.ServeHTTP(devRec, devReq)
	if devRec.Code != http.StatusOK {
		t.Fatalf("device_authorization: %d %s", devRec.Code, devRec.Body)
	}
	var devResp oauth.DeviceAuthResponse
	_ = json.Unmarshal(devRec.Body.Bytes(), &devResp)
	if devResp.DeviceCode == "" || devResp.UserCode == "" {
		t.Fatalf("unexpected device_authorization response: %+v", devResp)
	}

	// First poll: authorization_pending; immediate second poll: slow_down.
	pollDevice := func() *httptest.ResponseRecorder {
		pf := url.Values{"grant_type": {oauth.DeviceGrantType}, "client_id": {dcr.ClientID}, "device_code": {devResp.DeviceCode}}
		pr := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(pf.Encode()))
		pr.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, pr)
		return rec
	}
	if r := pollDevice(); r.Code != http.StatusBadRequest || !strings.Contains(r.Body.String(), `"authorization_pending"`) {
		t.Fatalf("first poll: %d %s", r.Code, r.Body)
	}
	if r := pollDevice(); r.Code != http.StatusBadRequest || !strings.Contains(r.Body.String(), `"slow_down"`) {
		t.Fatalf("rapid second poll: %d %s", r.Code, r.Body)
	}

	// Browser looks up user_code and approves with selected player.
	lookupRec := alice.do("GET", "/api/v1/oauth/device?user_code="+url.QueryEscape(devResp.UserCode), "")
	var devInfo DeviceCodeInfo
	_ = json.Unmarshal(lookupRec.Body.Bytes(), &devInfo)
	if lookupRec.Code != http.StatusOK || !devInfo.NeedsPlayerSelect || len(devInfo.Players) != 1 {
		t.Fatalf("device lookup: %d %+v", lookupRec.Code, devInfo)
	}
	appDevBody, _ := json.Marshal(map[string]any{"user_code": devResp.UserCode, "allow": true, "playerId": pResp.Player.ID})
	if r := alice.do("POST", "/api/v1/oauth/device", string(appDevBody)); r.Code != http.StatusOK {
		t.Fatalf("device approve: %d %s", r.Code, r.Body)
	}

	// Poll now succeeds with access_token + refresh_token + id_token.
	devTokRec := pollDevice()
	if devTokRec.Code != http.StatusOK {
		t.Fatalf("device token poll after approve: %d %s", devTokRec.Code, devTokRec.Body)
	}
	var devTok oauth.TokenResponse
	_ = json.Unmarshal(devTokRec.Body.Bytes(), &devTok)
	if !strings.HasPrefix(devTok.AccessToken, "slo_") || devTok.IDToken == "" {
		t.Fatalf("unexpected device token response: %+v", devTok)
	}

	// Deleting the bound player immediately revokes its OIDC token.
	if r := alice.do("DELETE", fmt.Sprintf("/api/v1/me/players/%d", pResp.Player.ID), ""); r.Code != http.StatusOK && r.Code != http.StatusNoContent {
		t.Fatalf("delete player: %d %s", r.Code, r.Body)
	}
	if r := launcher(t, h, "GET", "/oauth/userinfo", "", "Authorization", "Bearer "+devTok.AccessToken); r.Code != http.StatusUnauthorized {
		t.Fatalf("expected device OIDC token revoked when bound player deleted, got %d %s", r.Code, r.Body)
	}
}

func TestOIDCSecurityRegressions(t *testing.T) {
	d := newTestDeps(t)
	// Leave site.url empty initially to verify H6 Cache-Control: no-store + Vary: Host on discovery.
	setSettings(t, d, `{"captcha.provider":"none","site.url":"","oauth.enable_oidc":true,"oauth.enable_dcr":true}`)
	h, _ := New(d)
	setupSite(t, d, &browser{t: t, h: h})
	alice := registerUser(t, h, "alice-sec@example.com")

	// H6: Discovery document with blank site.url must not be cached by shared CDNs.
	discRec := launcher(t, h, "GET", "/.well-known/openid-configuration", "")
	if discRec.Code != http.StatusOK {
		t.Fatalf("discovery: %d %s", discRec.Code, discRec.Body)
	}
	if !strings.Contains(discRec.Header().Get("Cache-Control"), "no-store") || !strings.Contains(discRec.Header().Get("Vary"), "Host") {
		t.Fatalf("expected Cache-Control: no-store and Vary: Host when site.url is blank, got CC=%q Vary=%q",
			discRec.Header().Get("Cache-Control"), discRec.Header().Get("Vary"))
	}

	setSettings(t, d, `{"captcha.provider":"none","site.url":"https://skins.example.com","oauth.enable_oidc":true,"oauth.enable_dcr":true}`)

	var p1, p2 PlayerCreated
	_ = json.Unmarshal(alice.do("POST", "/api/v1/me/players", `{"name":"AlicePrimary"}`).Body.Bytes(), &p1)
	_ = json.Unmarshal(alice.do("POST", "/api/v1/me/players", `{"name":"AliceSecond"}`).Body.Bytes(), &p2)

	// Register a dynamic OIDC client.
	dcrRec := launcher(t, h, "POST", "/oauth/register", `{"client_name":"SecLauncher","redirect_uris":["http://127.0.0.1:41234/cb"],"grant_types":["authorization_code","refresh_token","urn:ietf:params:oauth:grant-type:device_code"],"token_endpoint_auth_method":"none"}`)
	if dcrRec.Code != http.StatusCreated {
		t.Fatalf("DCR failed: %d %s", dcrRec.Code, dcrRec.Body)
	}
	var dcr oauth.DCRResponse
	_ = json.Unmarshal(dcrRec.Body.Bytes(), &dcr)

	// H3: DCR client must be rejected for non-OIDC/admin scopes and plain PKCE.
	badScopeForm := url.Values{"client_id": {dcr.ClientID}, "scope": {"UsersManagement.ReadWrite"}}
	badDevReq := httptest.NewRequest(http.MethodPost, "/oauth/device_authorization", strings.NewReader(badScopeForm.Encode()))
	badDevReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	badDevRec := httptest.NewRecorder()
	h.ServeHTTP(badDevRec, badDevReq)
	if badDevRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when DCR client requests admin scope via device flow, got %d %s", badDevRec.Code, badDevRec.Body)
	}
	badPlainAuth := alice.do("GET", "/api/v1/oauth/authorize?"+url.Values{
		"client_id":             {dcr.ClientID},
		"redirect_uri":          {"http://127.0.0.1:41234/cb"},
		"response_type":         {"code"},
		"scope":                 {"openid profile"},
		"code_challenge":        {"plain-verifier-value-that-is-at-least-43-chars-long"},
		"code_challenge_method": {"plain"},
	}.Encode(), "")
	if !strings.Contains(badPlainAuth.Body.String(), "error=invalid_request") {
		t.Fatalf("expected error=invalid_request redirect when DCR client uses plain PKCE, got %d %s", badPlainAuth.Code, badPlainAuth.Body)
	}
	badAdminAuth := alice.do("GET", "/api/v1/oauth/authorize?"+url.Values{
		"client_id":             {dcr.ClientID},
		"redirect_uri":          {"http://127.0.0.1:41234/cb"},
		"response_type":         {"code"},
		"scope":                 {"openid UsersManagement.ReadWrite"},
		"code_challenge":        {"E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"},
		"code_challenge_method": {"S256"},
	}.Encode(), "")
	if !strings.Contains(badAdminAuth.Body.String(), "error=invalid_scope") {
		t.Fatalf("expected error=invalid_scope redirect when DCR client requests admin scope via authorize, got %d %s", badAdminAuth.Code, badAdminAuth.Body)
	}

	// Obtain a Yggdrasil.Server.Join-only OAuth token bound to p1 via Device Flow.
	devForm := url.Values{
		"client_id": {dcr.ClientID},
		"scope":     {"openid profile Yggdrasil.PlayerProfiles.Select Yggdrasil.Server.Join"},
	}
	devReq := httptest.NewRequest(http.MethodPost, "/oauth/device_authorization", strings.NewReader(devForm.Encode()))
	devReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	devRec := httptest.NewRecorder()
	h.ServeHTTP(devRec, devReq)
	if devRec.Code != http.StatusOK {
		t.Fatalf("device_authorization: %d %s", devRec.Code, devRec.Body)
	}
	var devResp oauth.DeviceAuthResponse
	_ = json.Unmarshal(devRec.Body.Bytes(), &devResp)

	appBody, _ := json.Marshal(map[string]any{"user_code": devResp.UserCode, "allow": true, "playerId": p1.Player.ID})
	if r := alice.do("POST", "/api/v1/oauth/device", string(appBody)); r.Code != http.StatusOK {
		t.Fatalf("first device approve failed: %d %s", r.Code, r.Body)
	}
	// C3: Second approval of the same user_code must be rejected atomically.
	if r := alice.do("POST", "/api/v1/oauth/device", string(appBody)); r.Code == http.StatusOK {
		t.Fatalf("expected second ApproveDeviceCode to fail, got 200 %s", r.Body)
	}

	// Poll token.
	pollForm := url.Values{"grant_type": {oauth.DeviceGrantType}, "client_id": {dcr.ClientID}, "device_code": {devResp.DeviceCode}}
	pollReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(pollForm.Encode()))
	pollReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	pollRec := httptest.NewRecorder()
	h.ServeHTTP(pollRec, pollReq)
	if pollRec.Code != http.StatusOK {
		t.Fatalf("device poll: %d %s", pollRec.Code, pollRec.Body)
	}
	var tok oauth.TokenResponse
	_ = json.Unmarshal(pollRec.Body.Bytes(), &tok)

	// C1: Yggdrasil.Server.Join token (without Yggdrasil.PlayerProfiles.Write) must NOT mutate p1 or p2 textures.
	if r := launcher(t, h, "DELETE", "/api/yggdrasil/api/user/profile/"+p1.Player.UUID+"/skin", "", "Authorization", "Bearer "+tok.AccessToken); r.Code != http.StatusForbidden {
		t.Fatalf("C1 regression: expected 403 on DELETE skin with Join-only OAuth token, got %d %s", r.Code, r.Body)
	}
	if r := launcher(t, h, "DELETE", "/api/yggdrasil/api/user/profile/"+p2.Player.UUID+"/skin", "", "Authorization", "Bearer "+tok.AccessToken); r.Code != http.StatusForbidden {
		t.Fatalf("C1 regression: expected 403 on DELETE another player's skin with Join-only OAuth token, got %d %s", r.Code, r.Body)
	}
}
