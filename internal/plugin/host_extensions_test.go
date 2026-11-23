package plugin

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent/auditlog"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/captcha"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/cdn"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/mail"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/notify"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/player"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/score"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/settings"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/texture"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/yggdrasil"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/migrate"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/pubsub"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/storage"
)

type fullTestEnv struct {
	db       *db.DB
	bus      *hook.Bus
	settings *settings.Service
	accounts *account.Service
	players  *player.Service
	textures *texture.Service
	notify   *notify.Service
	mailer   *mail.Mailer
	captcha  *captcha.Service
	cdn      *cdn.Service
	ygg      *yggdrasil.Service
	mgr      *Manager
	dir      string
}

func newFullTestEnv(t *testing.T) *fullTestEnv {
	t.Helper()
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	dir := t.TempDir()
	database, err := db.Open(ctx, db.Options{Driver: "sqlite", DSN: "full_plugin_test.db", DataDir: dir})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := migrate.Up(ctx, database, dir, log); err != nil {
		t.Fatalf("migrate.Up: %v", err)
	}

	broker := pubsub.New()
	t.Cleanup(broker.Close)
	bus := hook.NewBus(log, 4)
	t.Cleanup(func() { bus.Close(context.Background()) })

	reg := settings.NewRegistry()
	var defs []settings.Def
	defs = append(defs, account.SettingDefs()...)
	defs = append(defs, captcha.SettingDefs()...)
	defs = append(defs, mail.SettingDefs()...)
	defs = append(defs, score.SettingDefs()...)
	defs = append(defs, texture.SettingDefs()...)
	defs = append(defs, player.SettingDefs()...)
	defs = append(defs, yggdrasil.SettingDefs()...)
	defs = append(defs, cdn.SettingDefs()...)
	if err := reg.Register(defs...); err != nil {
		t.Fatalf("reg.Register: %v", err)
	}

	svc := settings.NewService(reg, database.Ent, broker, bus, log)
	if err := svc.Load(ctx); err != nil {
		t.Fatalf("settings.Load: %v", err)
	}
	store, err := storage.NewLocal(dir + "/textures")
	if err != nil {
		t.Fatalf("storage.NewLocal: %v", err)
	}

	accounts := account.NewService(database.Ent, log)
	players := player.NewService(database.Ent, log)
	textures := texture.NewService(database.Ent, store, log)
	notifySvc := notify.NewService(database.Ent, log)
	mailer := mail.New(log)
	captchaSvc := captcha.NewService(captcha.Options{Log: log})
	cdnSvc := cdn.NewService(bus, svc, log)
	yggSvc := yggdrasil.NewService(database.Ent, accounts, players, textures, log)
	yggSvc.KeyBits = 1024

	accounts.SetHooks(bus)
	players.SetHooks(bus)
	textures.SetHooks(bus)
	yggSvc.SetHooks(bus)

	reg.SetDynamicEnum("captcha.provider", captchaSvc.CustomProviders)
	reg.SetDynamicEnum("cdn.provider", cdnSvc.CustomProviders)
	if err := cdnSvc.Start(); err != nil {
		t.Fatalf("cdn.Start: %v", err)
	}

	mgr, err := NewManager(ManagerDeps{
		DB:        database.Ent,
		Bus:       bus,
		Settings:  svc,
		Accounts:  accounts,
		Players:   players,
		Textures:  textures,
		Notify:    notifySvc,
		Mailer:    mailer,
		Captcha:   captchaSvc,
		CDN:       cdnSvc,
		Yggdrasil: yggSvc,
		DataDir:   dir,
		Log:       log,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Shutdown(context.Background()) })

	return &fullTestEnv{
		db:       database,
		bus:      bus,
		settings: svc,
		accounts: accounts,
		players:  players,
		textures: textures,
		notify:   notifySvc,
		mailer:   mailer,
		captcha:  captchaSvc,
		cdn:      cdnSvc,
		ygg:      yggSvc,
		mgr:      mgr,
		dir:      dir,
	}
}

func makeSkinPNGBase64() string {
	img := image.NewRGBA(image.Rect(0, 0, 64, 32))
	img.Set(0, 0, color.RGBA{R: 120, G: 80, B: 200, A: 255})
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestHostAllRemainingCapabilities(t *testing.T) {
	env := newFullTestEnv(t)
	ctx := context.Background()

	// Seed a user with an external legacy salted hash and initial score.
	rawPw := "SecretPass#2026"
	salt := "somesalt"
	sum := sha256.Sum256([]byte(salt + ":" + rawPw))
	storedLegacy := salt + ":" + hex.EncodeToString(sum[:])

	u, err := env.db.Ent.User.Create().
		SetEmail("player1@example.com").
		SetEmailNormalized("player1@example.com").
		SetNickname("PlayerOne").
		SetPasswordHash(storedLegacy).
		SetPasswordAlgo("legacy-sha256").
		SetScore(500).
		SetStatus(user.StatusActive).
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	manifest := []byte(`{
		"id": "all-caps",
		"kind": "plugin",
		"version": "1.0.0",
		"name": "All Capabilities",
		"apiVersion": 1,
		"server": "server.js",
		"permissions": [
			"users.read", "users.write", "users.ban",
			"players.read", "players.write",
			"textures.read", "textures.write",
			"closet.write", "score.write",
			"notify.send", "mail.send",
			"auth.provider", "auth.password",
			"captcha.provider", "cdn.provider",
			"yggdrasil.extend", "cron", "storage.kv"
		],
		"services": ["addNums"]
	}`)
	pngB64 := makeSkinPNGBase64()
	serverJS := []byte(`
		export default {
			setup(ctx) {
				ctx.services.expose("addNums", async (a, b) => Number(a) + Number(b));

				ctx.cron.schedule("hourly-count", "0 * * * *", async () => {
					await ctx.kv.incr("cronRuns", 1);
				});

				ctx.auth.registerPasswordVerifier("legacy-sha256", async (storedHash, pw) => {
					const idx = String(storedHash).indexOf(":");
					if (idx < 0) return false;
					const salt = storedHash.slice(0, idx);
					const digest = storedHash.slice(idx + 1);
					const actual = await ctx.crypto.sha256(salt + ":" + pw);
					return await ctx.crypto.timingSafeEqual(actual, digest);
				});

				ctx.auth.registerProvider({
					id: "forum",
					name: "Forum SSO",
					mode: "credentials"
				}, async (input) => ({
					subject: "forum-" + String(input.username || "0"),
					email: String(input.username || "u") + "@forum.example",
					emailVerified: true,
					nickname: String(input.username || "ForumUser")
				}));

				ctx.captcha.registerProvider({ id: "plugin-turnstile", name: "Plugin Captcha" }, async (ans) => {
					return ans.value === "pass-token";
				});

				ctx.cdn.registerPurger({ id: "plugin-cdn", name: "Plugin CDN" }, async (urls) => {
					await ctx.kv.set("lastPurged", urls);
				});

				ctx.yggdrasil.extendMeta({
					skinDomains: ["custom-cdn.example.com"],
					meta: { customPlugin: "all-caps" }
				});

				ctx.http.post("/exercise", { auth: "public" }, async (req) => {
					const uid = req.body.userId;
					const fetched = await ctx.users.get(uid);
					await ctx.users.update(uid, { nickname: "UpdatedNick" });
					const p = await ctx.players.create(uid, "AlexHero");
					const renamed = await ctx.players.rename(uid, p.id, "SteveHero");
					const createdTex = await ctx.textures.create({
						uploaderId: uid,
						name: "HeroSkin",
						type: "steve",
						public: true,
						dataBase64: "` + pngB64 + `"
					});
					const bytes = await ctx.textures.readBytes(createdTex.texture.id);
					await ctx.players.setTextures(uid, renamed.id, { skin: createdTex.texture.id });
					const newBal = await ctx.score.apply(uid, 25, "plugin.bonus", "player", String(renamed.id));
					await ctx.notify.send(uid, "Welcome", "Hello from plugin");
					await ctx.mail.send({ to: fetched.email, subject: "Plugin Mail", text: "Hi!" });
					await ctx.audit.record("exercised", "user", String(uid), { ok: true });
					const cronRuns = await ctx.kv.get("cronRuns");
					return {
						nickBefore: fetched.nickname,
						playerName: renamed.name,
						texBytesLen: bytes ? bytes.length : 0,
						balanceAfter: newBal,
						cronRuns: cronRuns || 0
					};
				});
			}
		};
	`)

	if _, err := env.mgr.LoadFromBytes(ctx, manifest, serverJS, nil, nil, true); err != nil {
		t.Fatalf("LoadFromBytes all-caps: %v", err)
	}

	// 1. Exercise cron expression parser & cluster-unique cron execution
	if _, err := ParseCron("*/5 * * * *"); err != nil {
		t.Fatalf("ParseCron valid: %v", err)
	}
	if _, err := ParseCron("invalid"); err == nil {
		t.Fatalf("expected ParseCron invalid error")
	}
	slot := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC).Format("200601021504")
	if !env.mgr.claimCronSlot(ctx, "all-caps", "hourly-count", slot) {
		t.Fatalf("first worker claimCronSlot failed")
	}
	if env.mgr.claimCronSlot(ctx, "all-caps", "hourly-count", slot) {
		t.Fatalf("duplicate worker claimCronSlot must be rejected")
	}
	if err := env.mgr.TriggerCron(ctx, "all-caps", "hourly-count"); err != nil {
		t.Fatalf("TriggerCron: %v", err)
	}

	// 2. External password verifier + upgrade to argon2id on Login
	loggedIn, _, err := env.accounts.Login(ctx, account.LoginInput{
		Email:    "player1@example.com",
		Password: rawPw,
	}, account.LoginPolicy{}, account.RequestMeta{})
	if err != nil || loggedIn.ID != u.ID {
		t.Fatalf("Login with custom legacy-sha256 verifier failed: user=%v err=%v", loggedIn, err)
	}
	refetched, _ := env.accounts.GetUser(ctx, u.ID)
	if refetched.PasswordAlgo != account.AlgoArgon2id {
		t.Fatalf("expected legacy hash upgraded to argon2id, got %q", refetched.PasswordAlgo)
	}

	// 3. External auth provider registered in account.Service
	ap, ok := env.accounts.LookupAuthProvider("forum")
	if !ok || ap.Verify == nil {
		t.Fatalf("expected forum auth provider registered")
	}
	extUser, err := ap.Verify(ctx, map[string]any{"username": "steve"})
	if err != nil || extUser.Subject != "forum-steve" || extUser.Email != "steve@forum.example" {
		t.Fatalf("ext auth verify = %#v, err=%v", extUser, err)
	}

	// 4. Custom CAPTCHA provider via DynamicEnum & Verify
	_, err = env.settings.Set(ctx, nil, map[string]json.RawMessage{
		"captcha.provider": json.RawMessage(`"plugin-turnstile"`),
	}, nil)
	if err != nil {
		t.Fatalf("save dynamic captcha.provider: %v", err)
	}
	if err := env.captcha.Verify(ctx, captcha.Config{Provider: "plugin-turnstile"}, captcha.Answer{Value: "pass-token"}, "1.2.3.4"); err != nil {
		t.Fatalf("custom captcha Verify pass failed: %v", err)
	}
	if err := env.captcha.Verify(ctx, captcha.Config{Provider: "plugin-turnstile"}, captcha.Answer{Value: "wrong"}, "1.2.3.4"); err == nil {
		t.Fatalf("expected wrong captcha token to fail")
	}

	// 5. Custom CDN purger via DynamicEnum & Purge
	if _, err := env.settings.Set(ctx, nil, map[string]json.RawMessage{
		"site.url":     json.RawMessage(`"https://skin.example.com"`),
		"cdn.provider": json.RawMessage(`"plugin-cdn"`),
		"mail.driver":  json.RawMessage(`"log"`),
	}, nil); err != nil {
		t.Fatalf("save dynamic cdn.provider: %v", err)
	}
	if err := env.cdn.Purge(ctx, []string{"Alex"}); err != nil {
		t.Fatalf("cdn Purge: %v", err)
	}

	// 6. Yggdrasil MetaExtensions
	doms, meta := env.ygg.MetaExtensions(ctx)
	if len(doms) != 1 || doms[0] != "custom-cdn.example.com" || meta["customPlugin"] != "all-caps" {
		t.Fatalf("unexpected Yggdrasil MetaExtensions: domains=%v meta=%v", doms, meta)
	}

	// 7. Full /exercise HTTP call validating users, players, textures, score, notify, mail, audit, cron
	body := strings.NewReader(`{"userId": 1}`)
	req := httptest.NewRequest(http.MethodPost, "/api/plugins/all-caps/exercise", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	env.mgr.ServePluginAPI(rec, req, "all-caps", "/exercise", Viewer{}, "en")
	if rec.Code != http.StatusOK {
		t.Fatalf("/exercise failed (%d): %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode /exercise: %v", err)
	}
	if out["nickBefore"] != "PlayerOne" || out["playerName"] != "SteveHero" || out["texBytesLen"].(float64) == 0 || out["cronRuns"] != float64(1) {
		t.Fatalf("unexpected /exercise result: %#v", out)
	}
	auditCnt, _ := env.db.Ent.AuditLog.Query().Where(auditlog.Action("plugin.all-caps.exercised")).Count(ctx)
	if auditCnt != 1 {
		t.Fatalf("expected 1 plugin audit log, got %d", auditCnt)
	}

	// 8. Cross-plugin services (ctx.services.expose / ctx.services.use + dependency check)
	consumerManifest := []byte(`{
		"id": "svc-consumer",
		"kind": "plugin",
		"version": "1.0.0",
		"name": "Consumer",
		"apiVersion": 1,
		"server": "server.js",
		"dependencies": { "all-caps": "^1.0.0" }
	}`)
	consumerJS := []byte(`
		export default {
			setup(ctx) {
				const addNums = ctx.services.use("all-caps", "addNums");
				const undeclared = ctx.services.use("not-declared", "foo");
				ctx.http.get("/sum", { auth: "public" }, async () => {
					const sum = await addNums(19, 23);
					let denied = false;
					try { await undeclared(); } catch (e) { denied = String(e).includes("permission denied"); }
					return { sum, denied };
				});
			}
		};
	`)
	if _, err := env.mgr.LoadFromBytes(ctx, consumerManifest, consumerJS, nil, nil, true); err != nil {
		t.Fatalf("LoadFromBytes consumer: %v", err)
	}
	recSum := httptest.NewRecorder()
	env.mgr.ServePluginAPI(recSum, httptest.NewRequest(http.MethodGet, "/api/plugins/svc-consumer/sum", nil), "svc-consumer", "/sum", Viewer{}, "en")
	if recSum.Code != http.StatusOK {
		t.Fatalf("/sum failed (%d): %s", recSum.Code, recSum.Body.String())
	}
	var sumOut map[string]any
	_ = json.Unmarshal(recSum.Body.Bytes(), &sumOut)
	if sumOut["sum"] != float64(42) || sumOut["denied"] != true {
		t.Fatalf("unexpected cross-plugin service call output: %#v", sumOut)
	}

	// 9. Disabling all-caps cleanly unregisters external providers
	if err := env.mgr.Disable(ctx, "all-caps"); err != nil {
		t.Fatalf("Disable all-caps: %v", err)
	}
	if _, ok := env.accounts.LookupAuthProvider("forum"); ok {
		t.Fatalf("expected forum auth provider removed after Disable")
	}
	if got := env.captcha.CustomProviders(); len(got) != 0 {
		t.Fatalf("expected custom captcha providers empty after Disable, got %v", got)
	}
}

func TestHostSecurityAndEdgeRegressions(t *testing.T) {
	env := newFullTestEnv(t)
	ctx := context.Background()

	// 1. Built-in extensions cannot be overridden and cross-owner overwrites fail.
	if err := env.accounts.RegisterPasswordVerifier("plugin:p1", account.AlgoArgon2id, func(context.Context, string, string) (bool, error) {
		return true, nil
	}); err == nil {
		t.Fatal("expected error overriding built-in argon2id password verifier")
	}
	if err := env.accounts.RegisterPasswordVerifier("plugin:p1", "custom-legacy", func(context.Context, string, string) (bool, error) {
		return false, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := env.accounts.RegisterPasswordVerifier("plugin:p2", "custom-legacy", func(context.Context, string, string) (bool, error) {
		return true, nil
	}); err == nil {
		t.Fatal("expected error letting plugin:p2 overwrite plugin:p1 password verifier")
	}
	if err := env.captcha.RegisterProvider("plugin:p1", captcha.Turnstile, func(context.Context, captcha.Answer, string) error {
		return nil
	}); err == nil {
		t.Fatal("expected error overriding built-in turnstile captcha provider")
	}
	if err := env.cdn.RegisterPurger("plugin:p1", cdn.Cloudflare, nil); err == nil {
		t.Fatal("expected error overriding built-in cloudflare CDN provider")
	}

	// Seed admin, superadmin, and normal user with player + equipped skin.
	adm, err := env.db.Ent.User.Create().
		SetEmail("adm@example.com").SetEmailNormalized("adm@example.com").
		SetNickname("AdminUser").SetPasswordHash("x").SetPasswordAlgo("argon2id").
		SetRole(user.RoleAdmin).SetStatus(user.StatusActive).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sup, err := env.db.Ent.User.Create().
		SetEmail("sup@example.com").SetEmailNormalized("sup@example.com").
		SetNickname("SuperUser").SetPasswordHash("x").SetPasswordAlgo("argon2id").
		SetRole(user.RoleSuperadmin).SetStatus(user.StatusActive).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := env.db.Ent.User.Create().
		SetEmail("reg@example.com").SetEmailNormalized("reg@example.com").
		SetNickname("RegUser").SetPasswordHash("x").SetPasswordAlgo("argon2id").
		SetRole(user.RoleUser).SetScore(500).SetStatus(user.StatusActive).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Self-dependency in optionalDependencies must fail manifest validation.
	selfDepManifest := []byte(`{"id":"sec-probe","kind":"plugin","version":"1.0.0","name":"Self","apiVersion":1,"optionalDependencies":{"sec-probe":"^1.0.0"}}`)
	if _, err := ParseManifest(selfDepManifest); !errors.Is(err, ErrBadManifest) {
		t.Fatalf("expected ErrBadManifest on self-dependency, got %v", err)
	}

	manifest := []byte(`{
		"id": "sec-probe",
		"kind": "plugin",
		"version": "1.0.0",
		"name": "Security Probe",
		"apiVersion": 1,
		"server": "server.js",
		"web": "web.js",
		"services": ["ping"],
		"permissions": [
			"users.write", "users.ban",
			"players.read", "players.write",
			"textures.read", "textures.write",
			"closet.write", "notify.send", "mail.send",
			"captcha.provider"
		]
	}`)
	serverJS := `
		export default {
			setup(ctx) {
				ctx.services.expose("ping", () => "pong");
				ctx.captcha.registerProvider({ id: "fail-closed-undef", name: "Undef" }, () => undefined);
				ctx.captcha.registerProvider({ id: "fail-closed-obj", name: "Obj" }, () => ({ ok: "true" }));

				ctx.http.get("/status-json", { auth: "public" }, () => ({ status: 200, ok: true }));
				ctx.http.get("/html-xss", { auth: "public" }, () => ({
					status: 200,
					headers: { "Content-Type": "text/html", "Set-Cookie": "evil=1" },
					body: "<h1>hi</h1>"
				}));
				ctx.http.post("/clear-skin", { auth: "public" }, async () => {
					const created = await ctx.textures.create({
						uploaderId: ` + fmt.Sprintf("%d", reg.ID) + `,
						name: "cape_skin", type: "steve", dataBase64: "` + makeSkinPNGBase64() + `", public: true
					});
					const pl = await ctx.players.create(` + fmt.Sprintf("%d", reg.ID) + `, "TestHero");
					const equipped = await ctx.players.setTextures(` + fmt.Sprintf("%d", reg.ID) + `, pl.id, { skin: created.texture.id });
					const cleared = await ctx.players.setTextures(` + fmt.Sprintf("%d", reg.ID) + `, pl.id, { skin: null });
					return { before: equipped.skinId, after: cleared.skinId ?? null };
				});
				ctx.http.post("/priv-esc", { auth: "public" }, async () => {
					let updErr = "";
					try { await ctx.users.update(` + fmt.Sprintf("%d", adm.ID) + `, { nickname: "Hacked" }); } catch (e) { updErr = String(e); }
					let banErr = "";
					try { await ctx.users.ban(` + fmt.Sprintf("%d", sup.ID) + `, true); } catch (e) { banErr = String(e); }
					let mailErr = "";
					try { await ctx.mail.send({ to: "reg@example.com", template: "nonexistent_tpl" }); } catch (e) { mailErr = String(e); }
					let notifyErr = "";
					try { await ctx.notify.send([` + fmt.Sprintf("%d", reg.ID) + `, 0], "hi", "body"); } catch (e) { notifyErr = String(e); }
					await ctx.audit.record("spoof", "user", "1", { plugin: "fake-plugin" });
					return { updErr, banErr, mailErr, notifyErr };
				});
			}
		};
	`
	if _, err := env.mgr.LoadFromBytes(ctx, manifest, []byte(serverJS), map[string][]byte{"web.js": []byte(`export default {};`)}, nil, true); err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}

	// 2. CAPTCHA fail-closed on undefined / non-boolean ok.
	_, err = env.settings.Set(ctx, nil, map[string]json.RawMessage{
		"captcha.provider": json.RawMessage(`"fail-closed-undef"`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.captcha.Verify(ctx, captcha.ConfigFrom(env.settings.Current()), captcha.Answer{Value: "tok"}, "127.0.0.1"); !errors.Is(err, captcha.ErrInvalid) {
		t.Fatalf("expected ErrInvalid on undefined captcha result, got %v", err)
	}
	_, _ = env.settings.Set(ctx, nil, map[string]json.RawMessage{
		"captcha.provider": json.RawMessage(`"fail-closed-obj"`),
	}, nil)
	if err := env.captcha.Verify(ctx, captcha.ConfigFrom(env.settings.Current()), captcha.Answer{Value: "tok"}, "127.0.0.1"); !errors.Is(err, captcha.ErrInvalid) {
		t.Fatalf("expected ErrInvalid on string-ok captcha result, got %v", err)
	}

	// 3. Envelope heuristics & HTML sandbox CSP + nosniff.
	recJSON := httptest.NewRecorder()
	env.mgr.ServePluginAPI(recJSON, httptest.NewRequest(http.MethodGet, "/api/plugins/sec-probe/status-json", nil), "sec-probe", "/status-json", Viewer{}, "en")
	if recJSON.Code != http.StatusOK || !strings.Contains(recJSON.Body.String(), `"ok":true`) {
		t.Fatalf("expected {status:200, ok:true} preserved in JSON body, got %d %s", recJSON.Code, recJSON.Body.String())
	}
	if recJSON.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("expected X-Content-Type-Options: nosniff")
	}

	recHTML := httptest.NewRecorder()
	env.mgr.ServePluginAPI(recHTML, httptest.NewRequest(http.MethodGet, "/api/plugins/sec-probe/html-xss", nil), "sec-probe", "/html-xss", Viewer{}, "en")
	if recHTML.Header().Get("Content-Security-Policy") != pluginSandboxCSP || recHTML.Header().Get("Set-Cookie") != "" {
		t.Fatalf("expected sandbox CSP & stripped Set-Cookie on HTML route, csp=%q cookie=%q",
			recHTML.Header().Get("Content-Security-Policy"), recHTML.Header().Get("Set-Cookie"))
	}

	// 4. players.setTextures({ skin: null }) clears skin.
	recSkin := httptest.NewRecorder()
	env.mgr.ServePluginAPI(recSkin, httptest.NewRequest(http.MethodPost, "/api/plugins/sec-probe/clear-skin", nil), "sec-probe", "/clear-skin", Viewer{}, "en")
	if recSkin.Code != http.StatusOK {
		t.Fatalf("clear-skin = %d %s", recSkin.Code, recSkin.Body.String())
	}
	var skinOut map[string]any
	_ = json.Unmarshal(recSkin.Body.Bytes(), &skinOut)
	if skinOut["before"] == nil || skinOut["after"] != nil {
		t.Fatalf("expected skin cleared to null, got before=%v after=%v", skinOut["before"], skinOut["after"])
	}

	// 5. Privilege boundaries, mail template check, atomic notify validation, audit plugin preservation.
	recPriv := httptest.NewRecorder()
	env.mgr.ServePluginAPI(recPriv, httptest.NewRequest(http.MethodPost, "/api/plugins/sec-probe/priv-esc", nil), "sec-probe", "/priv-esc", Viewer{}, "en")
	if recPriv.Code != http.StatusOK {
		t.Fatalf("priv-esc = %d %s", recPriv.Code, recPriv.Body.String())
	}
	var privOut map[string]string
	_ = json.Unmarshal(recPriv.Body.Bytes(), &privOut)
	if privOut["updErr"] == "" || privOut["banErr"] == "" || privOut["mailErr"] == "" || privOut["notifyErr"] == "" {
		t.Fatalf("expected all security guard checks to reject, got %+v", privOut)
	}
	notifyCnt, _ := env.db.Ent.Notification.Query().Count(ctx)
	if notifyCnt != 0 {
		t.Fatalf("expected zero partial notifications sent on invalid batch, got %d", notifyCnt)
	}
	audits, err := env.db.Ent.AuditLog.Query().Where(auditlog.Action("plugin.sec-probe.spoof")).All(ctx)
	if err != nil || len(audits) != 1 || audits[0].Meta["plugin"] != "sec-probe" {
		t.Fatalf("expected audit meta.plugin locked to sec-probe, got %+v err=%v", audits, err)
	}

	// 6. Self-call guard in CallService + AssetHandler root script + disabled asset 404.
	if _, err := env.mgr.CallService(ctx, "sec-probe", "sec-probe", "ping"); err == nil {
		t.Fatal("expected error on self-service call")
	}
	ah := env.mgr.AssetHandler()
	recAssetOK := httptest.NewRecorder()
	ah.ServeHTTP(recAssetOK, httptest.NewRequest(http.MethodGet, "/plugins-assets/sec-probe/web.js", nil))
	if recAssetOK.Code != http.StatusOK || recAssetOK.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("expected 200 no-cache for unversioned web.js, got %d cache=%q", recAssetOK.Code, recAssetOK.Header().Get("Cache-Control"))
	}
	if err := env.mgr.Disable(ctx, "sec-probe"); err != nil {
		t.Fatal(err)
	}
	recAssetDis := httptest.NewRecorder()
	ah.ServeHTTP(recAssetDis, httptest.NewRequest(http.MethodGet, "/plugins-assets/sec-probe/web.js", nil))
	if recAssetDis.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on disabled plugin asset, got %d", recAssetDis.Code)
	}
}

func TestHighRiskYggdrasilAndPlayerOpsAndRouteOptions(t *testing.T) {
	env := newFullTestEnv(t)
	ctx := context.Background()

	u, err := env.db.Ent.User.Create().
		SetEmail("union@example.com").SetEmailNormalized("union@example.com").
		SetNickname("UnionUser").SetPasswordHash("x").SetPasswordAlgo("argon2id").
		SetRole(user.RoleUser).SetScore(500).SetStatus(user.StatusActive).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pl, _, err := env.players.Create(ctx, u.ID, "UnionHero", player.PolicyFrom(env.settings.Current()), score.Rates{})
	if err != nil {
		t.Fatal(err)
	}

	// Runtime without high-risk permissions must be denied setSigningKey and remapUUID.
	mLow, err := ParseManifest([]byte(`{
		"id": "low-priv",
		"kind": "plugin",
		"version": "1.0.0",
		"name": "Low Priv",
		"apiVersion": 1,
		"permissions": ["yggdrasil.extend", "players.read"]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	rtLow, err := newPluginRuntime(mLow, "", nil, nil, nil, nil, env.mgr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rtLow.hostYggdrasil(ctx, []any{"setSigningKey", "invalid"}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied on setSigningKey without yggdrasil.key.write, got %v", err)
	}
	if _, err := rtLow.hostPlayers(ctx, []any{"remapUUID", pl.UUID, "11111111222233334444555566667777"}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied on remapUUID without players.uuid.write, got %v", err)
	}
	if found, err := rtLow.hostPlayers(ctx, []any{"findByUUID", pl.UUID}); err != nil || found.(map[string]any)["name"] != "UnionHero" {
		t.Fatalf("findByUUID = %v, err=%v", found, err)
	}
	if listed, err := rtLow.hostPlayers(ctx, []any{"listAll", float64(0), float64(10)}); err != nil || len(listed.(map[string]any)["items"].([]map[string]any)) != 1 {
		t.Fatalf("listAll = %v, err=%v", listed, err)
	}

	// Runtime with high-risk permissions succeeds and records mandatory audit entries.
	mHigh, err := ParseManifest([]byte(`{
		"id": "high-priv",
		"kind": "plugin",
		"version": "1.0.0",
		"name": "High Priv",
		"apiVersion": 1,
		"permissions": ["routes.public", "yggdrasil.extend", "yggdrasil.key.write", "players.read", "players.uuid.write"],
		"routes": {"public": ["/api/union/*"]}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	rtHigh, err := newPluginRuntime(mHigh, "", nil, nil, nil, nil, env.mgr)
	if err != nil {
		t.Fatal(err)
	}

	k4096, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		t.Fatal(err)
	}
	pem4096 := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k4096)}))
	if _, err := rtHigh.hostYggdrasil(ctx, []any{"setSigningKey", pem4096}); err != nil {
		t.Fatalf("hostYggdrasil setSigningKey: %v", err)
	}
	if cnt, _ := env.db.Ent.AuditLog.Query().Where(auditlog.Action("plugin.yggdrasil.setSigningKey")).Count(ctx); cnt != 1 {
		t.Fatalf("expected 1 audit log for setSigningKey, got %d", cnt)
	}

	remappedUUID := "abcdef0123456789abcdef0123456789"
	if res, err := rtHigh.hostPlayers(ctx, []any{"remapUUID", pl.UUID, remappedUUID}); err != nil || res.(map[string]any)["uuid"] != remappedUUID {
		t.Fatalf("hostPlayers remapUUID = %v, err=%v", res, err)
	}
	if cnt, _ := env.db.Ent.AuditLog.Query().Where(auditlog.Action("plugin.player.remapUuid")).Count(ctx); cnt != 1 {
		t.Fatalf("expected 1 audit log for remapUUID, got %d", cnt)
	}

	// Verify OpenCORS, ForwardHeaders (with Cookie/X-SkinLoom-*/AdminToken scrubbing), and WebhookNoCSRF.
	var capturedReq map[string]any
	rtHigh.routes = []routeBinding{
		{
			Method:         http.MethodPost,
			Pattern:        "/api/union/member/sync",
			Auth:           "public",
			ForwardHeaders: []string{"X-Message-Signature", "Cookie", "X-SkinLoom-User-ID", "Authorization"},
			OpenCORS:       true,
			WebhookNoCSRF:  true,
			HandlerID:      "h1",
		},
	}
	rtHigh.driver = &stubHTTPDriver{fn: func(_ context.Context, _ string, req map[string]any) (any, error) {
		capturedReq = req
		return map[string]any{"ok": true}, nil
	}}
	env.mgr.mu.Lock()
	env.mgr.active["high-priv"] = rtHigh
	env.mgr.mu.Unlock()
	defer func() {
		env.mgr.mu.Lock()
		delete(env.mgr.active, "high-priv")
		env.mgr.mu.Unlock()
	}()

	if !env.mgr.IsWebhookNoCSRFRoute(http.MethodPost, "/api/union/member/sync") {
		t.Fatalf("expected IsWebhookNoCSRFRoute=true for /api/union/member/sync")
	}
	if env.mgr.IsWebhookNoCSRFRoute(http.MethodGet, "/api/union/member/sync") {
		t.Fatalf("expected IsWebhookNoCSRFRoute=false for wrong method")
	}

	// OPTIONS preflight on OpenCORS route returns 204 with ACAO: * and no ACAC.
	optRec := httptest.NewRecorder()
	env.mgr.ServePublicRoute(optRec, httptest.NewRequest(http.MethodOptions, "/api/union/member/sync", nil), Viewer{}, "en")
	if optRec.Code != http.StatusNoContent || optRec.Header().Get("Access-Control-Allow-Origin") != "*" || optRec.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("OPTIONS OpenCORS = %d headers=%v", optRec.Code, optRec.Header())
	}

	// POST with AdminToken viewer + sensitive headers: Viewer must be stripped, AdminToken Authorization/Cookie/X-SkinLoom-* scrubbed, X-Message-Signature forwarded.
	req := httptest.NewRequest(http.MethodPost, "/api/union/member/sync", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Message-Signature", "sig-value")
	req.Header.Set("Cookie", "skinloom_session=secret")
	req.Header.Set("X-SkinLoom-User-ID", "999")
	req.Header.Set("Authorization", "Bearer admin-token")
	postRec := httptest.NewRecorder()
	adminViewer := Viewer{Authenticated: true, UserID: 0, Role: "superadmin", Admin: true, Superadmin: true}
	env.mgr.ServePublicRoute(postRec, req, adminViewer, "en")
	if postRec.Code != http.StatusOK || postRec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("POST webhook = %d %s headers=%v", postRec.Code, postRec.Body.String(), postRec.Header())
	}
	if capturedReq["user"] != nil {
		t.Fatalf("expected WebhookNoCSRF to strip viewer, got %v", capturedReq["user"])
	}
	hdrs := capturedReq["headers"].(map[string]string)
	if hdrs["x-message-signature"] != "sig-value" {
		t.Fatalf("expected X-Message-Signature forwarded, got %v", hdrs)
	}
	if hdrs["cookie"] != "" || hdrs["x-skinloom-user-id"] != "" || hdrs["authorization"] != "" {
		t.Fatalf("expected sensitive headers scrubbed, got %v", hdrs)
	}

	// H1: Even when a browser cookie is present (UserID > 0), HasHostAuthHeader or slo_ bearer
	// and Proxy-Authorization must NEVER be forwarded to a plugin.
	reqDual := httptest.NewRequest(http.MethodPost, "/api/union/member/sync", strings.NewReader(`{}`))
	reqDual.Header.Set("Content-Type", "application/json")
	reqDual.Header.Set("Authorization", "Bearer admin-secret-token")
	reqDual.Header.Set("Proxy-Authorization", "Basic c2VjcmV0")
	reqDual.Header.Set("X-Message-Signature", "sig-2")
	rtHigh.routes[0].ForwardHeaders = append(rtHigh.routes[0].ForwardHeaders, "Proxy-Authorization")
	postRec2 := httptest.NewRecorder()
	dualViewer := Viewer{Authenticated: true, UserID: 42, Role: "superadmin", Admin: true, Superadmin: true, HasHostAuthHeader: true}
	env.mgr.ServePublicRoute(postRec2, reqDual, dualViewer, "en")
	hdrs2 := capturedReq["headers"].(map[string]string)
	if hdrs2["authorization"] != "" || hdrs2["proxy-authorization"] != "" {
		t.Fatalf("H1 regression: expected dual-auth Authorization and Proxy-Authorization scrubbed, got %v", hdrs2)
	}
	reqOAuth := httptest.NewRequest(http.MethodPost, "/api/union/member/sync", strings.NewReader(`{}`))
	reqOAuth.Header.Set("Authorization", "Bearer slo_host_oauth_secret")
	postRec3 := httptest.NewRecorder()
	env.mgr.ServePublicRoute(postRec3, reqOAuth, Viewer{}, "en")
	hdrs3 := capturedReq["headers"].(map[string]string)
	if hdrs3["authorization"] != "" {
		t.Fatalf("H1 regression: expected Bearer slo_ host token scrubbed, got %v", hdrs3)
	}

	// Verify hostSettings("patch", ...) persists schema-validated settings to DB and updates runtime snapshot.
	mSet, err := ParseManifest([]byte(`{
		"id": "mua-patch-test",
		"kind": "plugin",
		"version": "1.0.0",
		"name": "Patch Test",
		"apiVersion": 1,
		"settings": {
			"apiRoot": {"type": "string", "default": "https://default.example"},
			"secretKey": {"type": "secret", "default": ""},
			"enabled": {"type": "boolean", "default": false}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := env.db.Ent.Plugin.Create().
		SetID("mua-patch-test").SetKind("plugin").SetRuntime("js").SetVersion("1.0.0").
		SetStatus("enabled").SetGrantedPermissions([]string{}).
		SetSettings(map[string]any{"apiRoot": "https://default.example", "secretKey": "", "enabled": false}).
		SetSource("local").SetInstalledAt(now).SetUpdatedAt(now).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	env.mgr.mu.Lock()
	env.mgr.stored["mua-patch-test"] = &storedBundle{manifest: mSet}
	rtSet, err := newPluginRuntime(mSet, "", nil, nil, nil, nil, env.mgr)
	if err != nil {
		env.mgr.mu.Unlock()
		t.Fatal(err)
	}
	env.mgr.active["mua-patch-test"] = rtSet
	env.mgr.mu.Unlock()
	defer func() {
		env.mgr.mu.Lock()
		delete(env.mgr.active, "mua-patch-test")
		delete(env.mgr.stored, "mua-patch-test")
		env.mgr.mu.Unlock()
	}()

	patched, err := rtSet.hostSettings(ctx, []any{"patch", map[string]any{
		"apiRoot":   "https://skin.mualliance.ltd/api/union",
		"secretKey": "imported-secret",
		"enabled":   true,
	}})
	if err != nil {
		t.Fatalf("hostSettings patch failed: %v", err)
	}
	pm := patched.(map[string]any)
	if pm["apiRoot"] != "https://skin.mualliance.ltd/api/union" || pm["secretKey"] != "imported-secret" || pm["enabled"] != true {
		t.Fatalf("unexpected patched settings: %+v", pm)
	}
	row, err := env.db.Ent.Plugin.Get(ctx, "mua-patch-test")
	if err != nil || row.Settings["secretKey"] != "imported-secret" || row.Settings["enabled"] != true {
		t.Fatalf("persisted plugin settings mismatch: row=%+v err=%v", row, err)
	}

	// Verify DiscoverServiceProviders + bidirectional opt-in CallService:
	// "mig-provider" declares optionalDependencies: {"mig-hub": "^1.0.0"} and exposes "bsMigrateDescribe".
	// "mig-hub" can discover & call it WITHOUT hardcoding "mig-provider", whereas unrelated "mig-stranger" cannot.
	mHub, err := ParseManifest([]byte(`{"id":"mig-hub","kind":"plugin","version":"1.0.0","name":"Hub","apiVersion":1}`))
	if err != nil {
		t.Fatal(err)
	}
	mProv, err := ParseManifest([]byte(`{"id":"mig-provider","kind":"plugin","version":"1.0.0","name":"Prov","apiVersion":1,"optionalDependencies":{"mig-hub":"^1.0.0"},"services":["bsMigrateDescribe"]}`))
	if err != nil {
		t.Fatal(err)
	}
	mStranger, err := ParseManifest([]byte(`{"id":"mig-stranger","kind":"plugin","version":"1.0.0","name":"Stranger","apiVersion":1}`))
	if err != nil {
		t.Fatal(err)
	}
	rtHub, _ := newPluginRuntime(mHub, "", nil, nil, nil, nil, env.mgr)
	rtProv, _ := newPluginRuntime(mProv, "", nil, nil, nil, nil, env.mgr)
	rtStranger, _ := newPluginRuntime(mStranger, "", nil, nil, nil, nil, env.mgr)
	rtProv.services = []serviceBinding{{Name: "bsMigrateDescribe", HandlerID: "svc1"}}
	rtProv.driver = &stubServiceDriver{fn: func(_ context.Context, _ string, svcName, caller string, _ ...any) (any, error) {
		return map[string]any{"service": svcName, "caller": caller}, nil
	}}
	env.mgr.mu.Lock()
	env.mgr.active["mig-hub"] = rtHub
	env.mgr.active["mig-provider"] = rtProv
	env.mgr.active["mig-stranger"] = rtStranger
	env.mgr.mu.Unlock()
	defer func() {
		env.mgr.mu.Lock()
		delete(env.mgr.active, "mig-hub")
		delete(env.mgr.active, "mig-provider")
		delete(env.mgr.active, "mig-stranger")
		env.mgr.mu.Unlock()
	}()

	foundHub, err := env.mgr.DiscoverServiceProviders("mig-hub", "bsMigrateDescribe")
	if err != nil || len(foundHub) != 1 || foundHub[0] != "mig-provider" {
		t.Fatalf("DiscoverServiceProviders(mig-hub) = %v, err=%v", foundHub, err)
	}
	if res, err := env.mgr.CallService(ctx, "mig-hub", "mig-provider", "bsMigrateDescribe"); err != nil || res.(map[string]any)["caller"] != "mig-hub" {
		t.Fatalf("bidirectional CallService(mig-hub -> mig-provider) = %v, err=%v", res, err)
	}
	foundStranger, err := env.mgr.DiscoverServiceProviders("mig-stranger", "bsMigrateDescribe")
	if err != nil || len(foundStranger) != 0 {
		t.Fatalf("expected mig-stranger to discover 0 providers, got %v, err=%v", foundStranger, err)
	}
	if _, err := env.mgr.CallService(ctx, "mig-stranger", "mig-provider", "bsMigrateDescribe"); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied for mig-stranger -> mig-provider, got %v", err)
	}

	// H7: Cyclic CallService (mig-hub -> mig-provider -> mig-hub) must fail cleanly without deadlocking.
	rtHub.manifest.Services = []string{"pingBack"}
	rtHub.services = []serviceBinding{{Name: "pingBack", HandlerID: "svc-hub"}}
	rtHub.driver = &stubServiceDriver{fn: func(c context.Context, _ string, _, _ string, _ ...any) (any, error) {
		return env.mgr.CallService(c, "mig-hub", "mig-provider", "bsMigrateDescribe")
	}}
	rtProv.driver = &stubServiceDriver{fn: func(c context.Context, _ string, _, _ string, _ ...any) (any, error) {
		return env.mgr.CallService(c, "mig-provider", "mig-hub", "pingBack")
	}}
	if _, err := env.mgr.CallService(ctx, "mig-hub", "mig-provider", "bsMigrateDescribe"); err == nil || !strings.Contains(err.Error(), "cyclic") {
		t.Fatalf("H7 regression: expected cyclic error on recursive CallService, got %v", err)
	}

	// H2: UpdateSettings triggering a settings.onChange callback that calls hostSettings("patch", ...) must NOT deadlock.
	rtSet.driver = &stubSettingsDriver{fn: func(c context.Context, next map[string]any) {
		if next["apiRoot"] == "https://trigger.example" {
			_, _ = rtSet.hostSettings(c, []any{"patch", map[string]any{"enabled": false}})
		}
	}}
	done := make(chan error, 1)
	go func() {
		err := env.mgr.UpdateSettings(ctx, "mua-patch-test", map[string]any{
			"apiRoot":   "https://trigger.example",
			"secretKey": "imported-secret",
			"enabled":   true,
		})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("H2 regression: UpdateSettings with re-entrant Settings.Patch failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("H2 regression: UpdateSettings deadlocked when OnChange called Settings.Patch")
	}
}

type stubSettingsDriver struct {
	Driver
	fn func(ctx context.Context, next map[string]any)
}

func (s *stubSettingsDriver) NotifySettingsChanged(ctx context.Context, next map[string]any) error {
	s.fn(ctx, next)
	return nil
}

type stubServiceDriver struct {
	Driver
	fn func(ctx context.Context, handlerID, svcName, caller string, args ...any) (any, error)
}

func (s *stubServiceDriver) InvokeService(ctx context.Context, handlerID, svcName, caller string, args ...any) (any, error) {
	return s.fn(ctx, handlerID, svcName, caller, args...)
}

type stubHTTPDriver struct {
	Driver
	fn func(ctx context.Context, handlerID string, req map[string]any) (any, error)
}

func (s *stubHTTPDriver) InvokeHTTP(ctx context.Context, handlerID string, req map[string]any) (any, error) {
	return s.fn(ctx, handlerID, req)
}
