package bs

import (
	"encoding/json"
	"fmt"
	"html"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/Entropy-Increase-Team/skinloom/internal/modules/settings"
)

// BS options → SkinLoom settings (design 02 §4). An option is read the way BS's
// option() reads it: "true" and "false" are booleans, numbers are cast like PHP's
// (int), and an option missing from the table has its config/options.php default. A
// value equal to SkinLoom's default writes no row, so later changes to the default
// reach the site; a value SkinLoom cannot take keeps the default and says so.

// bsLocale is BS's locale (config/app.php): the language of options saved without one.
const bsLocale = "zh_CN"

// bsOptionDefaults is BS's config/options.php.
var bsOptionDefaults = map[string]string{
	"site_url": "", "site_name": "Blessing Skin", "site_description": "Open-source PHP Minecraft Skin Hosting Service",
	"register_with_player_name": "true", "require_verification": "false", "regs_per_ip": "3",
	"announcement": "Welcome to Blessing Skin {version}!", "home_pic_url": "./app/bg.webp", "custom_css": "", "custom_js": "",
	"player_name_rule": "official", "custom_player_name_regexp": "", "player_name_length_min": "3", "player_name_length_max": "16",
	"user_initial_score": "1000", "sign_gap_time": "24", "sign_score": "10,100", "score_per_storage": "true",
	"private_score_per_storage": "10", "return_score": "true", "score_per_player": "100", "sign_after_zero": "false",
	"copyright_text": "", "auto_del_invalid_texture": "false", "allow_downloading_texture": "true", "texture_name_regexp": "",
	"cache_expire_time": "31536000", "max_upload_file_size": "1024", "force_ssl": "false", "auto_detect_asset_url": "true",
	"copyright_prefer": "0", "score_per_closet_item": "0", "favicon_url": "app/favicon.ico", "score_award_per_texture": "0",
	"take_back_scores_after_deletion": "true", "score_award_per_like": "0", "meta_keywords": "", "meta_description": "",
	"meta_extras": "", "cdn_address": "", "recaptcha_sitekey": "", "recaptcha_secretkey": "", "recaptcha_invisible": "false",
	"reporter_score_modification": "0", "reporter_reward_score": "0", "content_policy": "", "transparent_navbar": "false",
	"status_code_for_private": "403", "navbar_color": "cyan", "sidebar_color": "dark-maroon", "max_texture_width": "2048",
}

// BS's own defaults, as its install wrote them: they become SkinLoom's defaults.
var (
	defaultAnnouncement = regexp.MustCompile(`^Welcome to Blessing Skin (\{version\}|[0-9][0-9A-Za-z.+-]*)!$`)
	defaultCopyright    = regexp.MustCompile(`^<b>Copyright &copy; [0-9]{4} <a href="\{site_url\}">\{site_name\}</a>\.</b> All rights reserved\.$`)
)

// optionMapper writes SkinLoom settings from BS options and reports what it could not.
type optionMapper struct {
	reg     *settings.Registry
	opts    map[string]string
	locales []string // SkinLoom's
	out     map[string]json.RawMessage
	r       *Report
}

// raw is the option's string, or its BS default.
func (m *optionMapper) raw(name string) string {
	if v, ok := m.opts[name]; ok {
		return v
	}
	return bsOptionDefaults[name]
}

// value is option(name): true, false, nil or the string.
func (m *optionMapper) value(name string) any { return phpOption(m.raw(name)) }

func (m *optionMapper) bool(name string) bool { return phpBool(m.value(name)) }

// int is (int) option(name); a value that is not a clean integer is reported.
func (m *optionMapper) int(name string) int64 {
	v := m.value(name)
	n, clean := phpInt(v)
	if !clean && m.raw(name) != bsOptionDefaults[name] { // BS's own "true" is 1
		m.r.add(Fixed, "options", 0, "%s = %q is not a whole number: taken as %d, as BS did", name, m.raw(name), n)
	}
	return n
}

func (m *optionMapper) str(name string) string {
	if s, ok := m.value(name).(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// set stores v as SkinLoom setting key, unless it is the default or not valid there.
func (m *optionMapper) set(from, key string, v any) {
	def, ok := m.reg.Def(key)
	if !ok {
		panic("bs: unknown setting " + key) // the mapping and the registry disagree: a test catches it
	}
	if l, ok := v.(map[string]string); ok && len(l) == 0 {
		// No text in any language (BS's own default was left out): keep SkinLoom's
		// default rather than blank it.
		delete(m.out, key)
		return
	}
	raw, err := json.Marshal(v)
	if err == nil {
		raw, err = def.Canonical(raw)
	}
	if err != nil {
		m.r.add(Warning, "options", 0, "%s: %s cannot be used as %s (%v): SkinLoom's default is kept", from, short(fmt.Sprint(v)), key, err)
		delete(m.out, key)
		return
	}
	if dflt, _ := json.Marshal(def.Default); settings.SameJSON(raw, dflt) {
		delete(m.out, key)
		return
	}
	m.out[key] = raw
}

// drop reports an option SkinLoom has no setting for, when the site changed it from
// BS's default.
func (m *optionMapper) drop(name, why string) {
	if _, set := m.opts[name]; !set || m.raw(name) == bsOptionDefaults[name] {
		return
	}
	m.r.add(Dropped, "options", 0, "%s = %s: %s", name, short(m.raw(name)), why)
}

// localized collects option name and its per-language copies (BS saves name_<locale>
// next to name), keyed by SkinLoom locale. name alone is what BS showed in every
// language without its own copy; it goes to BS's locale, from which SkinLoom falls
// back the same way. keep filters and rewrites each text ("" leaves it out).
func (m *optionMapper) localized(name string, keep func(string) string) map[string]string {
	out := map[string]string{}
	for _, l := range m.locales {
		if v, ok := m.opts[name+"_"+bsLocaleOf(l)]; ok {
			if v = keep(v); v != "" {
				out[l] = v
			}
		}
	}
	for _, k := range sortedKeys(m.opts) {
		v := m.opts[k]
		if loc, ok := strings.CutPrefix(k, name+"_"); ok && v != "" && localeOf(loc) != "" && !containsLocale(m.locales, localeOf(loc)) {
			m.r.add(Dropped, "options", 0, "%s: SkinLoom has no %s interface yet", k, loc)
		}
	}
	if base := keep(m.raw(name)); base != "" {
		if l := localeOf(bsLocale); out[l] == "" {
			out[l] = base
		}
	}
	return out
}

// planSettings maps BS options and the mail settings of .env to SkinLoom settings.
func planSettings(d *sourceData, o planOptions, r *Report) map[string]json.RawMessage {
	m := &optionMapper{reg: o.Settings, opts: d.Options, locales: o.Locales, out: map[string]json.RawMessage{}, r: r}
	trim := strings.TrimSpace

	// Site.
	siteName := m.localized("site_name", trim)
	m.set("site_name", "site.name", siteName)
	m.set("site_description", "site.description", m.localized("site_description", func(s string) string {
		if s = trim(s); s == bsOptionDefaults["site_description"] {
			return "" // BS's own tagline: SkinLoom's describes the site better
		}
		return s
	}))
	// BS trims the address as it saves it; older values may still end in /index.php.
	siteURL := strings.TrimSuffix(strings.TrimRight(m.str("site_url"), "/"), "/index.php")
	if siteURL != "" {
		m.set("site_url", "site.url", siteURL)
	}
	m.set("announcement", "site.announcement", m.localized("announcement", func(s string) string {
		if s = trim(s); defaultAnnouncement.MatchString(s) {
			return ""
		}
		return s
	}))
	m.set("content_policy", "texture.content_policy", m.localized("content_policy", trim))
	converted := false
	m.set("copyright_text", "site.copyright", m.localized("copyright_text", func(s string) string {
		if s = trim(s); s == "" || defaultCopyright.MatchString(s) {
			return ""
		}
		converted = true
		return htmlToMarkdown(s, settings.PickLocalized(siteName, localeOf(bsLocale)), siteURL)
	}))
	if converted {
		r.add(Fixed, "options", 0, "copyright_text: BS's HTML became Markdown, with {site_name} and {site_url} filled in; check the footer")
	}
	if c := m.raw("copyright_prefer"); c != "0" && c != "" {
		r.add(Dropped, "options", 0, "copyright_prefer: the footer's \"Powered by\" line is SkinLoom's own")
	}
	if f := m.str("favicon_url"); f != "" && f != bsOptionDefaults["favicon_url"] {
		if isHTTPURL(f) {
			m.set("favicon_url", "site.favicon", f)
		} else {
			r.add(Warning, "options", 0, "favicon_url = %s is a file of the BS site: put the icon on this site or a web address and set it in the site settings", short(f))
		}
	}
	if tz := o.Location.String(); tz != "" && tz != "Local" {
		m.set("--timezone", "site.timezone", tz)
	}

	// Registration and sign-in.
	m.set("require_verification", "auth.require_email_verification", m.bool("require_verification"))
	m.set("regs_per_ip", "auth.registrations_per_ip", m.int("regs_per_ip"))
	if m.bool("register_with_player_name") {
		r.add(Dropped, "options", 0, "register_with_player_name: SkinLoom's registration does not ask for a player name; new users add one in the user center")
	}
	site, secret := m.str("recaptcha_sitekey"), m.str("recaptcha_secretkey")
	if (site == "") != (secret == "") {
		r.add(Warning, "options", 0, "recaptcha_sitekey/recaptcha_secretkey: only one is set, which left BS's captcha unusable; SkinLoom uses its image captcha until both keys are set")
	}
	if site != "" && secret != "" {
		m.set("recaptcha_sitekey", "captcha.provider", "recaptcha")
		m.set("recaptcha_sitekey", "captcha.recaptcha.site_key", site)
		m.set("recaptcha_secretkey", "captcha.recaptcha.secret_key", secret)
		if m.bool("recaptcha_invisible") {
			r.add(Fixed, "options", 0, "recaptcha_invisible: SkinLoom shows the reCAPTCHA v2 checkbox; the keys work for both if they were made for the checkbox")
		}
	}

	// Players.
	rule := m.str("player_name_rule")
	m.set("player_name_rule", "player.name_rule", rule)
	// An empty pattern is BS's /.*/, as it is SkinLoom's: any name.
	if pattern := m.str("custom_player_name_regexp"); rule == "custom" && pattern != "" {
		re, ok := phpRegexp(pattern)
		if !ok {
			r.add(Warning, "options", 0, "custom_player_name_regexp = %s cannot be converted to Go's syntax: player names follow the official rule until it is set again", short(m.raw("custom_player_name_regexp")))
			delete(m.out, "player.name_rule")
		} else {
			m.set("custom_player_name_regexp", "player.name_regexp", re)
		}
	}
	lo, hi := clamp(m.int("player_name_length_min"), 1, 32), clamp(m.int("player_name_length_max"), 1, 32)
	if lo > hi {
		lo, hi = hi, lo
	}
	if lo != m.int0("player_name_length_min") || hi != m.int0("player_name_length_max") {
		r.add(Fixed, "options", 0, "player_name_length_min/max = %s/%s: set to %d/%d (SkinLoom allows 1 to 32 characters)", m.raw("player_name_length_min"), m.raw("player_name_length_max"), lo, hi)
	}
	m.set("player_name_length_min", "player.name_length_min", lo)
	m.set("player_name_length_max", "player.name_length_max", hi)

	// Score.
	m.set("user_initial_score", "score.initial", m.int("user_initial_score"))
	m.set("sign_gap_time", "score.sign_gap_hours", m.int("sign_gap_time"))
	m.set("sign_after_zero", "score.sign_daily", m.bool("sign_after_zero"))
	if from, to, ok := strings.Cut(m.raw("sign_score"), ","); ok {
		a, _ := phpInt(phpOption(from))
		b, _ := phpInt(phpOption(to))
		if a > b {
			a, b = b, a
		}
		m.set("sign_score", "score.sign_min", a)
		m.set("sign_score", "score.sign_max", b)
	} else {
		r.add(Warning, "options", 0, "sign_score = %s is not \"from,to\": SkinLoom's default check-in range is kept", short(m.raw("sign_score")))
	}
	m.set("score_per_storage", "score.per_storage", m.int("score_per_storage"))
	m.set("private_score_per_storage", "score.private_per_storage", m.int("private_score_per_storage"))
	m.set("score_per_closet_item", "score.per_closet_item", m.int("score_per_closet_item"))
	m.set("score_per_player", "score.per_player", m.int("score_per_player"))
	m.set("return_score", "score.return_on_delete", m.bool("return_score"))
	m.set("score_award_per_texture", "score.award_per_texture", m.int("score_award_per_texture"))
	m.set("score_award_per_like", "score.award_per_like", m.int("score_award_per_like"))
	m.set("reporter_score_modification", "score.reporter_modification", m.int("reporter_score_modification"))
	m.set("reporter_reward_score", "score.reporter_reward", m.int("reporter_reward_score"))
	if !m.bool("take_back_scores_after_deletion") {
		m.drop("take_back_scores_after_deletion", "SkinLoom always takes a like's award back when the texture leaves the closet")
	}

	// Textures.
	if kb := m.int("max_upload_file_size"); kb > 8192 {
		r.add(Fixed, "options", 0, "max_upload_file_size = %d KB: SkinLoom allows at most 8192", kb)
		m.set("max_upload_file_size", "texture.max_size_kb", 8192)
	} else {
		m.set("max_upload_file_size", "texture.max_size_kb", kb)
	}
	if w := m.int("max_texture_width"); w != clamp(w, 64, 2048) {
		r.add(Fixed, "options", 0, "max_texture_width = %d: SkinLoom allows 64 to 2048 pixels, set to %d", w, clamp(w, 64, 2048))
		m.set("max_texture_width", "texture.max_width", clamp(w, 64, 2048))
	} else {
		m.set("max_texture_width", "texture.max_width", w)
	}
	if re := m.str("texture_name_regexp"); re != "" {
		if g, ok := phpRegexp(re); ok {
			m.set("texture_name_regexp", "texture.name_regexp", g)
		} else {
			r.add(Warning, "options", 0, "texture_name_regexp = %s cannot be converted to Go's syntax: texture names are not checked until it is set again", short(re))
		}
	}
	m.set("allow_downloading_texture", "texture.allow_download", m.bool("allow_downloading_texture"))
	m.set("status_code_for_private", "texture.private_status", m.int("status_code_for_private"))

	// Appearance and SEO.
	m.set("custom_css", "theme.custom_css", m.str("custom_css"))
	m.set("meta_keywords", "seo.keywords", m.localized("meta_keywords", trim))
	if desc := m.str("meta_description"); desc != "" {
		r.add(Dropped, "options", 0, "meta_description: SkinLoom's search description is the site description; copy it there if it was different")
	}
	// BS printed it in a <script> at the end of every page; SkinLoom loads it as a file
	// with the page's CSP nonce (ADR 0039).
	if js := m.str("custom_js"); js != "" {
		m.set("custom_js", "theme.custom_js", js)
		r.add(Warning, "options", 0, "custom_js: SkinLoom switches pages without reloading, so the script runs once when the site is "+
			"opened, not on every page; code that changes a page has to watch for navigation")
	}
	m.drop("meta_extras", "SkinLoom does not insert raw HTML into the page head; analytics and verification tags come with plugins")
	m.drop("home_pic_url", "set it as the background of the hero block in the home page editor")
	for _, name := range []string{"transparent_navbar", "navbar_color", "sidebar_color", "hide_intro", "fixed_bg"} {
		m.drop(name, "BS's layout options do not apply to SkinLoom's themes; colors are theme tokens")
	}

	// Delivery.
	m.drop("cache_expire_time", "SkinLoom's profile cache time (delivery.profile_cache_seconds) defaults to 60 seconds: a year kept old skins in caches")
	m.drop("cdn_address", "this was where BS's own scripts and styles came from; a CDN in front of the site is set with cdn.base_url")
	m.drop("auto_del_invalid_texture", "SkinLoom does not delete textures whose file is missing; the system status page counts them")

	// Yggdrasil (the plugin's options, when it was installed).
	alg := d.Options["ygg_uuid_algorithm"]
	if alg == "v4" || (alg == "" && d.UUIDTable) {
		m.set("ygg_uuid_algorithm", "yggdrasil.uuid_algorithm", "v4")
	}
	for _, o := range [][2]string{
		{"ygg_token_expire_1", "yggdrasil.token_valid_seconds"}, {"ygg_token_expire_2", "yggdrasil.token_refresh_seconds"},
		{"ygg_tokens_limit", "yggdrasil.tokens_per_user"}, {"ygg_search_profile_max", "yggdrasil.search_profile_max"},
	} {
		if _, ok := d.Options[o[0]]; ok {
			m.set(o[0], o[1], m.int(o[0]))
		}
	}
	if _, ok := d.Options["ygg_enable_ali"]; ok {
		m.set("ygg_enable_ali", "yggdrasil.enable_ali", m.bool("ygg_enable_ali"))
	}
	if s := m.str("ygg_skin_domain"); s != "" {
		var domains []string
		for p := range strings.SplitSeq(s, ",") {
			if p = strings.TrimSpace(p); p != "" {
				domains = append(domains, p)
			}
		}
		m.set("ygg_skin_domain", "yggdrasil.skin_domains", strings.Join(domains, ","))
	}

	if ov := planOverrides(d, m.locales, r); len(ov) > 0 {
		m.set("language_lines", "i18n.overrides", ov)
	}
	planMail(m, o.Mail)
	return m.out
}

// planMail maps the MAIL_* settings of .env.
func planMail(m *optionMapper, c *Mail) {
	if c == nil {
		m.r.add(Warning, ".env", 0, "mail settings are in BS's .env: give it with --env to import them, or set mail up in the site settings")
		return
	}
	switch {
	case c.Mailer == "log":
		m.set("MAIL_MAILER", "mail.driver", "log")
		return
	case c.Mailer != "smtp":
		m.r.add(Warning, ".env", 0, "MAIL_MAILER = %s: SkinLoom sends mail over SMTP; set it up in the site settings", c.Mailer)
		return
	case !c.HostSet:
		return // never configured: Laravel's default host is a placeholder
	}
	port, err := strconv.Atoi(c.Port)
	if err != nil {
		m.r.add(Warning, ".env", 0, "MAIL_PORT = %s is not a number: set mail up in the site settings", short(c.Port))
		return
	}
	// Laravel 10 hands Symfony Mailer the port alone: implicit TLS on 465, STARTTLS when
	// the server offers it elsewhere. MAIL_ENCRYPTION=ssl on 587 still means STARTTLS.
	enc := "starttls"
	switch {
	case port == 465:
		enc = "tls"
	case c.Encryption == "" && port == 25:
		enc = "none"
	}
	from := fromHeader(c.FromName, c.FromAddress)
	m.set("MAIL_MAILER", "mail.driver", "smtp")
	m.set("MAIL_HOST", "mail.smtp.host", c.Host)
	m.set("MAIL_PORT", "mail.smtp.port", port)
	m.set("MAIL_ENCRYPTION", "mail.smtp.encryption", enc)
	m.set("MAIL_USERNAME", "mail.smtp.username", c.Username)
	m.set("MAIL_PASSWORD", "mail.smtp.password", c.Password)
	m.set("MAIL_FROM_ADDRESS", "mail.from", from)
	if _, ok := m.out["mail.driver"]; ok {
		m.r.add(Fixed, ".env", 0, "mail: SMTP %s:%d with %s; send a test mail from the site settings", c.Host, port, enc)
	}
}

// fromHeader is the sender as an admin writes it: "Name" <address>. Go's
// mail.Address.String encodes a name like 皮肤站 as =?utf-8?q?...?=, which the site
// settings would show as is; the mailer encodes it when it sends.
func fromHeader(name, address string) string {
	a, err := mail.ParseAddress(address)
	if err != nil || name == "" {
		return address // the setting's validator reports a bad address
	}
	q := `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(name) + `" <` + a.Address + ">"
	if b, err := mail.ParseAddress(q); err == nil && b.Name == name && b.Address == a.Address {
		return q
	}
	return (&mail.Address{Name: name, Address: a.Address}).String()
}

// phpOption is what BS's option() returns for a stored string.
func phpOption(s string) any {
	switch strings.ToLower(s) {
	case "true", "(true)":
		return true
	case "false", "(false)":
		return false
	case "null", "(null)":
		return nil
	}
	return s
}

// phpBool is PHP's truthiness of an option value.
func phpBool(v any) bool {
	switch v := v.(type) {
	case bool:
		return v
	case string:
		return v != "" && v != "0"
	}
	return false
}

// phpInt is PHP's (int) cast: leading whitespace, a sign and digits; anything else
// counts as 0. clean is false when that dropped part of the value.
func phpInt(v any) (n int64, clean bool) {
	switch v := v.(type) {
	case bool:
		if v {
			return 1, false
		}
		return 0, false
	case nil:
		return 0, false
	}
	s := strings.TrimSpace(v.(string))
	end := 0
	if end < len(s) && (s[end] == '-' || s[end] == '+') {
		end++
	}
	digits := end
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == digits {
		return 0, false
	}
	n, err := strconv.ParseInt(s[:end], 10, 64)
	return n, err == nil && end == len(s)
}

// int0 is int without the report: for comparing.
func (m *optionMapper) int0(name string) int64 {
	n, _ := phpInt(m.value(name))
	return n
}

func clamp(n, lo, hi int64) int64 { return min(max(n, lo), hi) }

// phpRegexp converts a PCRE pattern with delimiters (BS passes options to preg_match)
// to Go's RE2. Patterns RE2 cannot express (look-around, backreferences) are not
// converted.
func phpRegexp(p string) (string, bool) {
	p = strings.TrimSpace(p)
	if len(p) < 2 {
		return "", false
	}
	open := p[0]
	closeBy := map[byte]byte{'(': ')', '{': '}', '[': ']', '<': '>'}
	end := open
	if c, ok := closeBy[open]; ok {
		end = c
	}
	if open >= 'a' && open <= 'z' || open >= 'A' && open <= 'Z' || open >= '0' && open <= '9' || open == '\\' {
		return "", false
	}
	i := strings.LastIndexByte(p, end)
	if i <= 0 {
		return "", false
	}
	body, mods := p[1:i], p[i+1:]
	flags := ""
	for _, f := range mods {
		switch f {
		case 'i', 'm', 's':
			flags += string(f)
		case 'u', 'D':
			// RE2 is always UTF-8; $ never matches before a final newline unless m.
		case 'x':
			return "", false
		default:
			return "", false
		}
	}
	if flags != "" {
		body = "(?" + flags + ")" + body
	}
	if _, err := regexp.Compile(body); err != nil {
		return "", false
	}
	return body, true
}

var (
	htmlLink  = regexp.MustCompile(`(?is)<a\s[^>]*?href\s*=\s*["']([^"']*)["'][^>]*>(.*?)</a>`)
	htmlBold  = regexp.MustCompile(`(?is)<(?:b|strong)>(.*?)</(?:b|strong)>`)
	htmlItal  = regexp.MustCompile(`(?is)<(?:i|em)>(.*?)</(?:i|em)>`)
	htmlBreak = regexp.MustCompile(`(?i)<br\s*/?>`)
	htmlTag   = regexp.MustCompile(`(?s)<[^>]*>`)
)

// htmlToMarkdown turns BS's copyright HTML into the Markdown SkinLoom renders (no raw
// HTML): links, bold, italics and line breaks are kept, other tags dropped.
func htmlToMarkdown(s, siteName, siteURL string) string {
	s = strings.NewReplacer("{site_name}", siteName, "{site_url}", siteURL).Replace(s)
	s = htmlLink.ReplaceAllString(s, "[$2]($1)")
	s = htmlBold.ReplaceAllString(s, "**$1**")
	s = htmlItal.ReplaceAllString(s, "*$1*")
	s = htmlBreak.ReplaceAllString(s, "  \n")
	s = htmlTag.ReplaceAllString(s, "")
	return strings.TrimSpace(html.UnescapeString(s))
}

func isHTTPURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

var bsLocaleName = regexp.MustCompile(`^[a-z]{2}(_[A-Z]{2})?$`)

// localeOf turns a BS locale (zh_CN) into SkinLoom's (zh-CN); "" when it is not one.
func localeOf(bs string) string {
	if !bsLocaleName.MatchString(bs) {
		return ""
	}
	return strings.ReplaceAll(bs, "_", "-")
}

func bsLocaleOf(l string) string { return strings.ReplaceAll(l, "-", "_") }

func containsLocale(list []string, l string) bool {
	for _, x := range list {
		if x == l {
			return true
		}
	}
	return false
}

// short quotes a value for the report, cut to a line.
func short(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len([]rune(s)) > 60 {
		s = string([]rune(s)[:57]) + "..."
	}
	return strconv.Quote(s)
}

// sameJSON: a and b decode to the same value.
