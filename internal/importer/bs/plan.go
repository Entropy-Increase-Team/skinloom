package bs

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Entropy-Increase-Team/skinloom/internal/modules/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/player"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/settings"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/clip"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/storage"
)

// The SkinLoom rows an import writes. IDs of users, textures, players and reports are
// BS's, so /skinlib/show/{tid} and every other link keeps working.
type (
	tUser struct {
		ID                                       int64
		Email, EmailNormalized, Nickname         string
		PasswordHash, PasswordAlgo, Role, Status string
		Locale, RegisterIP                       string
		Score                                    int64
		Avatar                                   *int64
		VerifiedAt, LastSignAt                   *time.Time
		CreatedAt                                time.Time
	}
	tBlob struct {
		Hash string
		// File is the BS file it comes from ("" when missing); it differs from Hash when
		// the content no longer matches the name.
		File          string
		Size          int64
		Width, Height int
		Refs          int
	}
	tTexture struct {
		ID         int64
		Name, Type string
		Hash       string
		Size       int64
		Uploader   *int64
		Public     bool
		Likes      int
		CreatedAt  time.Time
	}
	tPlayer struct {
		ID, UserID int64
		Name, UUID string
		Skin, Cape *int64
		UpdatedAt  time.Time
	}
	tCloset struct {
		UserID, TextureID int64
		Name              string
	}
	tReport struct {
		ID                          int64
		Texture, Uploader, Reporter *int64
		Reason, Status              string
		CreatedAt                   time.Time
	}
	tNotification struct {
		UserID         int64
		Title, Content string
		CreatedAt      time.Time
	}
)

type planned struct {
	Users         []tUser
	Blobs         []tBlob
	Textures      []tTexture
	Players       []tPlayer
	Closet        []tCloset
	Reports       []tReport
	Notifications []tNotification
	Clients       []tClient
	// Settings are the setting rows to write (key → JSON value), SigningKey the
	// Yggdrasil key as PKCS #8 PEM ("" to let SkinLoom make one).
	Settings    map[string]json.RawMessage
	SigningKey  string
	Superadmins int
}

type planOptions struct {
	Location       *time.Location
	PasswordMethod string
	Salt           string
	// Transition is PASSWORD_METHODS with its salts, used when BS has the
	// password-algorithm-transition plugin enabled.
	Transition []Method
	// Locales SkinLoom has messages for (zh-CN, en...); other BS locales become "follow
	// the browser".
	Locales []string
	Now     time.Time
	// Settings checks the settings an import writes; Mail is BS's mail configuration.
	Settings *settings.Registry
	Mail     *Mail
}

// Lengths the SkinLoom schema allows, in characters.
const (
	maxNickname     = 64
	maxTextureName  = 50
	maxReportReason = 500
	maxNoticeTitle  = 64
)

// plan turns the BS rows into SkinLoom rows, applying the default policy of 02 §5 to
// dirty data and recording every change in the report. It reads nothing and writes
// nothing, so the dry run and the import plan alike.
func plan(d *sourceData, files map[string]fileInfo, o planOptions, r *Report) *planned {
	p := &planned{}
	at := func(w wall) time.Time {
		if t, ok := w.in(o.Location); ok {
			return t
		}
		return o.Now
	}
	// text drops NUL bytes and invalid UTF-8, which BS stores as given but PostgreSQL
	// and MySQL refuse: one such byte would fail the whole import.
	text := func(table string, id int64, field, v string) string {
		c := cleanText(v)
		if c != v {
			r.add(Fixed, table, id, "%s had invalid characters: replaced", field)
		}
		return c
	}
	methods := []Method{{o.PasswordMethod, o.Salt}}
	if len(o.Transition) > 0 && pluginEnabled(d.Options["plugins_enabled"], "password-algorithm-transition") {
		methods = o.Transition
	}

	// Users.
	users := map[int64]int{} // BS uid → index in p.Users
	avatars := map[int64]int64{}
	byEmail := map[string]int64{}
	for _, u := range d.Users {
		email := strings.TrimSpace(text("users", u.ID, "email", u.Email))
		norm := account.NormalizeEmail(email)
		switch {
		case email == "":
			r.add(Blocking, "users", u.ID, "no email address: give the account one in BS, or delete it")
			continue
		case len(email) > 255:
			r.add(Blocking, "users", u.ID, "email address longer than 255 bytes")
			continue
		}
		if other, dup := byEmail[norm]; dup {
			r.add(Blocking, "users", u.ID, "same email address as user %d (%s): SkinLoom does not tell addresses apart by case; change or merge one of the accounts in BS", other, email)
			continue
		}
		byEmail[norm] = u.ID
		t := tUser{ID: u.ID, Email: email, EmailNormalized: norm, Score: u.Score, CreatedAt: at(u.RegisteredAt)}
		switch u.Permission {
		case -1:
			t.Role, t.Status = "user", "banned"
		case 0:
			t.Role, t.Status = "user", "active"
		case 1:
			t.Role, t.Status = "admin", "active"
		case 2:
			t.Role, t.Status = "superadmin", "active"
			p.Superadmins++
		default:
			t.Role, t.Status = "user", "active"
			r.add(Warning, "users", u.ID, "unknown permission %d: imported as a normal user", u.Permission)
		}
		t.Nickname = clip.Runes(strings.TrimSpace(text("users", u.ID, "nickname", u.Nickname)), maxNickname)
		if t.Nickname == "" {
			local, _, _ := strings.Cut(email, "@")
			t.Nickname = clip.Runes(cmp.Or(local, "user"+strconv.FormatInt(u.ID, 10)), account.MaxNicknameLen)
			r.add(Fixed, "users", u.ID, "no nickname: set to %q", t.Nickname)
		}
		algo, hash, problem := passwordOf(u.Password, methods)
		t.PasswordAlgo, t.PasswordHash = algo, hash
		if problem != "" {
			r.add(Warning, "users", u.ID, "%s: if the user cannot sign in, they reset the password by email", problem)
		}
		if l := strings.ReplaceAll(u.Locale, "_", "-"); slices.Contains(o.Locales, l) {
			t.Locale = l
		}
		t.RegisterIP = clip.Bytes(cleanText(u.IP), 45)
		if u.Verified {
			v := t.CreatedAt // BS keeps no verification time
			t.VerifiedAt = &v
		}
		if s, ok := u.LastSignAt.in(o.Location); ok {
			t.LastSignAt = &s
		}
		if u.Avatar > 0 {
			avatars[u.ID] = u.Avatar
		}
		users[u.ID] = len(p.Users)
		p.Users = append(p.Users, t)
	}
	userExists := func(id int64) bool { _, ok := users[id]; return ok }

	// Textures and their files. A user's uploads of the same file are merged into the
	// first one: SkinLoom keeps one texture per user and file.
	textures := map[int64]int{} // tid → index in p.Textures
	merged := map[int64]int64{} // merged-away tid → the tid kept
	type ownedFile struct {
		user int64
		hash string
	}
	owned := map[ownedFile]int64{}
	blobs := map[string]int{} // hash → index in p.Blobs
	reported := map[string]bool{}
	for _, s := range d.Textures {
		if s.Type != "steve" && s.Type != "alex" && s.Type != "cape" {
			r.add(Dropped, "textures", s.ID, "unknown type %q", s.Type)
			continue
		}
		if !storage.ValidKey(s.Hash) {
			r.add(Dropped, "textures", s.ID, "hash %q is not a SHA-256 hash", s.Hash)
			continue
		}
		fi := files[s.Hash]
		hash := s.Hash
		if fi.Problem != "" {
			if !reported[s.Hash] {
				r.add(Warning, "textures", s.ID, "%s (%s): imported without a file, it answers 404 as it did in BS; put the file into SkinLoom's texture directory under this name", fi.Problem, s.Hash)
				reported[s.Hash] = true
			}
		} else if fi.Hash != s.Hash {
			hash = fi.Hash
			if !reported[s.Hash] {
				r.add(Fixed, "textures", s.ID, "the file %s does not match its name (changed after upload): stored as %s", s.Hash, fi.Hash)
				reported[s.Hash] = true
			}
		}
		var uploader *int64
		if userExists(s.Uploader) {
			uploader = &s.Uploader
			key := ownedFile{s.Uploader, hash}
			if kept, dup := owned[key]; dup {
				merged[s.ID] = kept
				k := &p.Textures[textures[kept]]
				if (k.Type == "cape") != (s.Type == "cape") {
					r.add(Fixed, "textures", s.ID, "user %d uploaded the same file as texture %d, once as a %s and once as a %s: merged into it, players wearing this one as a %s lose it", s.Uploader, kept, k.Type, s.Type, s.Type)
				} else {
					r.add(Fixed, "textures", s.ID, "user %d uploaded the same file as texture %d: merged into it", s.Uploader, kept)
				}
				if s.Public && !k.Public {
					k.Public = true
					r.add(Fixed, "textures", kept, "made public: merged with the public texture %d", s.ID)
				}
				continue
			}
			owned[key] = s.ID
		} else if s.Uploader != 0 {
			r.add(Fixed, "textures", s.ID, "uploader %d no longer exists: shown as a deleted account", s.Uploader)
		}
		name := clip.Runes(strings.TrimSpace(text("textures", s.ID, "name", s.Name)), maxTextureName)
		if name == "" {
			name = "#" + strconv.FormatInt(s.ID, 10)
			r.add(Fixed, "textures", s.ID, "no name: named %q", name)
		}
		size := fi.Size
		if fi.Problem != "" {
			size = s.SizeKB * 1024
		}
		if i, ok := blobs[hash]; ok {
			p.Blobs[i].Refs++
		} else {
			blobs[hash] = len(p.Blobs)
			b := tBlob{Hash: hash, Size: size, Width: fi.Width, Height: fi.Height, Refs: 1}
			if fi.Problem == "" {
				b.File = s.Hash
			}
			p.Blobs = append(p.Blobs, b)
		}
		textures[s.ID] = len(p.Textures)
		p.Textures = append(p.Textures, tTexture{ID: s.ID, Name: name, Type: s.Type, Hash: hash, Size: size,
			Uploader: uploader, Public: s.Public, CreatedAt: at(s.UploadedAt)})
	}
	// texture resolves a BS reference: 0 and -1 are "none", merged textures point to
	// the one kept.
	texture := func(tid int64) (*tTexture, bool) {
		if kept, ok := merged[tid]; ok {
			tid = kept
		}
		i, ok := textures[tid]
		if !ok {
			return nil, false
		}
		return &p.Textures[i], true
	}

	for i := range p.Users {
		u := &p.Users[i]
		tid, ok := avatars[u.ID]
		if !ok {
			continue
		}
		if t, ok := texture(tid); ok && t.Type != "cape" {
			u.Avatar = &t.ID
		} else {
			r.add(Fixed, "users", u.ID, "avatar texture %d is gone or a cape: cleared", tid)
		}
	}

	// Players. Names first, in pid order (the earliest keeps a name); UUIDs afterwards.
	type uuidRow struct {
		id   int64
		uuid string
	}
	exact, folded := map[string]uuidRow{}, map[string]uuidRow{}
	for _, row := range d.UUIDs { // in id order: the first row of a name wins
		name := strings.TrimSpace(row.Name)
		v := uuidRow{row.ID, strings.ToLower(strings.ReplaceAll(strings.TrimSpace(row.UUID), "-", ""))}
		if _, ok := exact[name]; !ok {
			exact[name] = v
		}
		if _, ok := folded[player.Key(name)]; !ok {
			folded[player.Key(name)] = v
		}
	}
	type candidate struct {
		row       uuidRow
		ok, exact bool
	}
	var cands []candidate // one per p.Players
	takenNames := map[string]int64{}
	for _, s := range d.Players {
		if !userExists(s.UserID) {
			r.add(Dropped, "players", s.ID, "owner %d no longer exists", s.UserID)
			continue
		}
		name := strings.TrimSpace(text("players", s.ID, "name", s.Name))
		if name == "" {
			r.add(Dropped, "players", s.ID, "no name")
			continue
		}
		final := clip.Runes(name, player.MaxNameLen)
		if other, taken := takenNames[player.Key(final)]; taken || final != s.Name {
			final = freeName(final, takenNames)
			switch {
			case taken:
				r.add(Fixed, "players", s.ID, "name %q is taken by player %d (names are not case-sensitive): renamed to %q, the owner is notified", s.Name, other, final)
			case utf8.RuneCountInString(name) > player.MaxNameLen:
				r.add(Fixed, "players", s.ID, "name %q is longer than %d characters: renamed to %q, the owner is notified", s.Name, player.MaxNameLen, final)
			default:
				r.add(Fixed, "players", s.ID, "name %q has spaces at its ends or invalid characters: renamed to %q, the owner is notified", s.Name, final)
			}
			p.Notifications = append(p.Notifications, renameNotice(s.UserID, s.Name, final, o.Now))
		}
		takenNames[player.Key(final)] = s.ID

		t := tPlayer{ID: s.ID, UserID: s.UserID, Name: final, UpdatedAt: at(s.ModifiedAt)}
		for _, slot := range []struct {
			tid  int64
			cape bool
			set  **int64
		}{{s.Skin, false, &t.Skin}, {s.Cape, true, &t.Cape}} {
			if slot.tid <= 0 {
				continue
			}
			tex, ok := texture(slot.tid)
			switch {
			case !ok:
				r.add(Fixed, "players", s.ID, "texture %d is gone: cleared", slot.tid)
			case (tex.Type == "cape") != slot.cape:
				r.add(Fixed, "players", s.ID, "texture %d (%s) is in the wrong slot: cleared", slot.tid, tex.Type)
			default:
				*slot.set = &tex.ID
			}
		}
		var c candidate
		if row, ok := exact[name]; ok {
			c = candidate{row, true, true}
		} else if row, ok := folded[player.Key(name)]; ok {
			c = candidate{row, true, false}
		}
		cands = append(cands, c)
		p.Players = append(p.Players, t)
	}

	// UUIDs: the uuid table's rows matched by exact name go first, over all players,
	// then those matched case-insensitively, and only then are new ones generated. A
	// player who used a UUID in game keeps it, even when an earlier player's name folds
	// to the same or would generate the same.
	usedRows := map[int64]bool{}
	usedUUIDs := map[string]int64{}
	for _, exactPass := range []bool{true, false} {
		for i, c := range cands {
			pl := &p.Players[i]
			if !c.ok || c.exact != exactPass || pl.UUID != "" {
				continue
			}
			switch {
			case usedRows[c.row.id]:
				// another player of the same name has it
			case !validUUID(c.row.uuid):
				r.add(Fixed, "players", pl.ID, "UUID %q in the uuid table is not a UUID: a new one is given", c.row.uuid)
			case usedUUIDs[c.row.uuid] != 0:
				r.add(Fixed, "players", pl.ID, "UUID %s also belongs to player %d: a new one is given", c.row.uuid, usedUUIDs[c.row.uuid])
			default:
				pl.UUID = c.row.uuid
				usedRows[c.row.id] = true
				usedUUIDs[pl.UUID] = pl.ID
			}
		}
	}
	// As the Yggdrasil plugin would on first use: it gives v3 UUIDs only when
	// ygg_uuid_algorithm says v3. Without the plugin, v3 matches offline-mode servers.
	alg := d.Options["ygg_uuid_algorithm"]
	v4 := alg == "v4" || (alg == "" && d.UUIDTable)
	for i := range p.Players {
		pl := &p.Players[i]
		if pl.UUID != "" {
			continue
		}
		if pl.UUID = player.OfflineUUID(pl.Name); v4 || usedUUIDs[pl.UUID] != 0 {
			pl.UUID = player.RandomUUID()
		}
		usedUUIDs[pl.UUID] = pl.ID
	}
	if n := len(d.UUIDs) - len(usedRows); n > 0 {
		r.add(Dropped, "uuid", 0, "%d rows were not used: their player was deleted or renamed, or its UUID went to an earlier player", n)
	}

	// Closet. BS's table has no key, so a texture could be in a closet twice.
	type closetKey struct{ user, texture int64 }
	inCloset := map[closetKey]bool{}
	dangling := 0
	for _, s := range d.Closet {
		tex, ok := texture(s.TextureID)
		if !ok || !userExists(s.UserID) {
			dangling++
			continue
		}
		k := closetKey{s.UserID, tex.ID}
		if inCloset[k] {
			r.add(Dropped, "user_closet", s.UserID, "texture %d is in the closet twice: kept once", s.TextureID)
			continue
		}
		inCloset[k] = true
		name := clip.Runes(strings.TrimSpace(text("user_closet", s.UserID, "item name", s.Name)), maxTextureName)
		if name == "" {
			name = tex.Name
		}
		tex.Likes++
		p.Closet = append(p.Closet, tCloset{UserID: s.UserID, TextureID: tex.ID, Name: name})
	}
	if dangling > 0 {
		r.add(Dropped, "user_closet", 0, "%d rows name a user or texture that no longer exists", dangling)
	}

	// Reports.
	type reportKey struct{ reporter, texture int64 }
	reportedBy := map[reportKey]int64{}
	for _, s := range d.Reports {
		t := tReport{ID: s.ID, CreatedAt: at(s.ReportedAt)}
		if tex, ok := texture(s.TextureID); ok {
			t.Texture = &tex.ID
		}
		if userExists(s.Uploader) {
			t.Uploader = &s.Uploader
		}
		if userExists(s.Reporter) {
			t.Reporter = &s.Reporter
		}
		if t.Texture != nil && t.Reporter != nil {
			k := reportKey{*t.Reporter, *t.Texture}
			if first, dup := reportedBy[k]; dup {
				r.add(Dropped, "reports", s.ID, "user %d already reported texture %d (report %d)", *t.Reporter, *t.Texture, first)
				continue
			}
			reportedBy[k] = s.ID
		}
		t.Reason = cmp.Or(clip.Runes(strings.TrimSpace(text("reports", s.ID, "reason", s.Reason)), maxReportReason), "-")
		switch s.Status {
		case 0:
			t.Status = "pending"
		case 1:
			t.Status = "resolved"
		case 2:
			t.Status = "rejected"
		default:
			t.Status = "pending"
			r.add(Warning, "reports", s.ID, "unknown status %d: imported as pending", s.Status)
		}
		p.Reports = append(p.Reports, t)
	}

	// Unread notifications.
	badNotices := 0
	for _, s := range d.Notifications {
		var data struct{ Title, Content string }
		if !userExists(s.UserID) || json.Unmarshal([]byte(s.Data), &data) != nil {
			badNotices++
			continue
		}
		title := clip.Runes(strings.TrimSpace(text("notifications", s.UserID, "title", data.Title)), maxNoticeTitle)
		if title == "" {
			title = "-"
		}
		content := text("notifications", s.UserID, "content", data.Content)
		p.Notifications = append(p.Notifications, tNotification{UserID: s.UserID, Title: title, Content: content, CreatedAt: at(s.CreatedAt)})
	}
	if badNotices > 0 {
		r.add(Dropped, "notifications", 0, "%d unread notifications belong to no user or cannot be read", badNotices)
	}

	if p.Superadmins == 0 && len(p.Users) > 0 {
		r.add(Warning, "users", 0, "no super administrator (permission 2): open the setup page after the import to create one")
	}
	p.Clients = planClients(d, userExists, at, r)
	p.SigningKey = planSigningKey(d, r)
	planPlugins(d, r)
	planTables(d, r)
	if o.Settings != nil { // Run always has it; unit tests of the rows may leave it out
		p.Settings = planSettings(d, o, r)
	}

	for _, m := range d.Missing {
		r.add(Warning, m, 0, "table not found: nothing imported from it (fine if the site never had it)")
	}
	return p
}

// passwordOf works out how a BS password hash was made. password_hash() results name
// their algorithm; hex digests are told apart by length, salted or not by the site's
// method. With the password-algorithm-transition plugin, methods lists every method in
// use (the new one first, each with its own salt) and one site has a mix of them.
func passwordOf(stored string, methods []Method) (algo, hash, problem string) {
	s := strings.TrimSpace(stored)
	switch {
	case s == "":
		return "unknown", "", "no password"
	case strings.HasPrefix(s, "$2y$"), strings.HasPrefix(s, "$2a$"), strings.HasPrefix(s, "$2b$"):
		return account.AlgoBcrypt, s, ""
	case strings.HasPrefix(s, "$argon2id$"):
		return account.AlgoArgon2id, s, ""
	case strings.HasPrefix(s, "$argon2i$"):
		return account.AlgoArgon2i, s, ""
	}
	family := map[int]string{32: "MD5", 64: "SHA256", 128: "SHA512"}[len(s)]
	if family == "" || !isHex(s) {
		return "unknown", s, "password hash in an unknown format"
	}
	var names, fits []string
	salts := map[string]string{}
	for _, m := range methods {
		name := strings.ToUpper(cmp.Or(strings.TrimSpace(m.Name), "BCRYPT"))
		names = append(names, name)
		if strings.TrimPrefix(name, "SALTED2") == family && !slices.Contains(fits, name) {
			fits = append(fits, name)
			salts[name] = m.Salt
		}
	}
	lower := strings.ToLower(family)
	if len(fits) == 0 {
		return lower, s, fmt.Sprintf("an unsalted %s digest, but the password method is %s (taken as unsalted; wrong if the site once used a salted method)", family, strings.Join(names, ", "))
	}
	if len(fits) > 1 {
		problem = fmt.Sprintf("a %s digest that %s could have made (PASSWORD_METHODS): taken as %s", family, strings.Join(fits, " or "), fits[0])
	}
	if fits[0] == family {
		return lower, s, problem
	}
	salt := salts[fits[0]]
	if len(s)+1+len(salt) > 255 {
		return "unknown", s, "SALT is too long"
	}
	return "salted2" + lower, s + ":" + salt, problem
}

// pluginEnabled reads BS's plugins_enabled option: a JSON list of {"name": ...}.
func pluginEnabled(option, name string) bool {
	var list []json.RawMessage
	if json.Unmarshal([]byte(option), &list) != nil {
		return false
	}
	for _, raw := range list {
		var p struct{ Name string }
		if json.Unmarshal(raw, &p) == nil && p.Name == name {
			return true
		}
	}
	return false
}

func isHex(s string) bool {
	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

func validUUID(s string) bool { return len(s) == 32 && isHex(s) && strings.ToLower(s) == s }

// freeName returns name, or name with the first free suffix _2, _3... (shortened to
// stay within the length limit), that taken does not hold.
func freeName(name string, taken map[string]int64) string {
	if _, ok := taken[player.Key(name)]; !ok {
		return name
	}
	for n := 2; ; n++ {
		suffix := "_" + strconv.Itoa(n)
		c := clip.Runes(name, player.MaxNameLen-len(suffix)) + suffix
		if _, ok := taken[player.Key(c)]; !ok {
			return c
		}
	}
}

func renameNotice(userID int64, from, to string, now time.Time) tNotification {
	f, t := mdEscape(from), mdEscape(to)
	return tNotification{UserID: userID, CreatedAt: now, Title: "角色已改名 / Player renamed",
		Content: fmt.Sprintf("本站迁移到 SkinLoom 时，角色名 **%s** 不能继续使用（与另一个角色重名、超过 32 个字符，或首尾有空格；角色名不区分大小写），已改名为 **%s**。可以在用户中心的角色管理里换一个名字。\n\n"+
			"When this site moved to SkinLoom, your player name **%s** could not be kept (another player had the same name, it was longer than 32 characters, or it had spaces at its ends; names are not case-sensitive) and was changed to **%s**. You can choose another name under Players in the user center.", f, t, f, t)}
}

// mdEscape keeps a name from being read as Markdown.
func mdEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune("\\`*_{}[]()<>#+-.!|~", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// cleanText drops NUL bytes and replaces invalid UTF-8.
func cleanText(s string) string {
	return strings.ToValidUTF8(strings.ReplaceAll(s, "\x00", ""), "\uFFFD")
}
