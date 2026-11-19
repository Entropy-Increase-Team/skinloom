package plugin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/audit"
)

type authProviderBinding struct {
	Def       account.AuthProviderDef
	VerifyFn  int // -1 if redirect-only
	HandlerID string
}

type passwordVerifierBinding struct {
	Algo      string
	FnID      int
	HandlerID string
}

func serializeUserDTO(u *ent.User) map[string]any {
	if u == nil {
		return nil
	}
	out := map[string]any{
		"id":            u.ID,
		"email":         u.Email,
		"nickname":      u.Nickname,
		"role":          string(u.Role),
		"status":        string(u.Status),
		"score":         u.Score,
		"locale":        u.Locale,
		"emailVerified": u.EmailVerifiedAt != nil,
		"createdAt":     u.CreatedAt.UTC().Format(time.RFC3339),
	}
	if u.AvatarTextureID != nil {
		out["avatarTextureId"] = *u.AvatarTextureID
	} else {
		out["avatarTextureId"] = nil
	}
	if u.LastLoginAt != nil {
		out["lastLoginAt"] = u.LastLoginAt.UTC().Format(time.RFC3339)
	} else {
		out["lastLoginAt"] = nil
	}
	return out
}

func (r *pluginRuntime) hostUsers(ctx context.Context, args []any) (any, error) {
	if r.accounts == nil {
		return nil, errors.New("account service unavailable")
	}
	op, _ := argAt(args, 0).(string)
	switch op {
	case "get":
		if err := r.perms.Check(PermUsersRead); err != nil {
			return nil, err
		}
		uid := int64(asFloat(argAt(args, 1)))
		u, err := r.accounts.GetUser(ctx, uid)
		if ent.IsNotFound(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return serializeUserDTO(u), nil

	case "find":
		if err := r.perms.Check(PermUsersRead); err != nil {
			return nil, err
		}
		email, _ := argAt(args, 1).(string)
		u, err := r.accounts.GetUserByEmail(ctx, email)
		if ent.IsNotFound(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return serializeUserDTO(u), nil

	case "list":
		if err := r.perms.Check(PermUsersRead); err != nil {
			return nil, err
		}
		opts, _ := asMap(argAt(args, 1))
		f := account.UserFilter{}
		if opts != nil {
			f.Query, _ = opts["q"].(string)
			f.Role, _ = opts["role"].(string)
			f.Status, _ = opts["status"].(string)
			f.Before = int64(asFloat(opts["before"]))
			f.Offset = int(asFloat(opts["offset"]))
			f.Limit = int(asFloat(opts["limit"]))
			if f.Limit <= 0 {
				f.Limit = int(asFloat(opts["perPage"]))
			}
			if page := int(asFloat(opts["page"])); page > 1 && f.Offset == 0 && f.Limit > 0 {
				f.Offset = (page - 1) * f.Limit
			}
		}
		rows, nextBefore, err := r.accounts.ListUsers(ctx, f)
		if err != nil {
			return nil, err
		}
		items := make([]map[string]any, 0, len(rows))
		for _, u := range rows {
			items = append(items, serializeUserDTO(u))
		}
		return map[string]any{"items": items, "nextBefore": nextBefore}, nil

	case "update":
		if err := r.perms.Check(PermUsersWrite); err != nil {
			return nil, err
		}
		uid := int64(asFloat(argAt(args, 1)))
		patch, ok := asMap(argAt(args, 2))
		if !ok {
			return nil, errors.New("users.update requires patch object")
		}
		target, err := r.accounts.GetUser(ctx, uid)
		if err != nil {
			return nil, err
		}
		if target.Role != user.RoleUser {
			return nil, fmt.Errorf("%w: plugins cannot modify administrator accounts", account.ErrForbidden)
		}
		up := account.AdminUpdate{}
		if v, has := patch["nickname"].(string); has {
			up.Nickname = &v
		}
		if v, has := patch["verified"].(bool); has {
			up.EmailVerified = &v
		}
		if v, has := patch["emailVerified"].(bool); has {
			up.EmailVerified = &v
		}
		u, err := r.accounts.AdminUpdateUser(ctx, nil, uid, up, account.RequestMeta{})
		if err != nil {
			return nil, err
		}
		if loc, has := patch["locale"].(string); has && loc != "" {
			u, err = r.accounts.UpdateProfile(ctx, uid, account.ProfileUpdate{Locale: &loc}, []string{"zh-CN", "en"})
			if err != nil {
				return nil, err
			}
		}
		return serializeUserDTO(u), nil

	case "ban":
		if err := r.perms.Check(PermUsersBan); err != nil {
			return nil, err
		}
		uid := int64(asFloat(argAt(args, 1)))
		target, err := r.accounts.GetUser(ctx, uid)
		if err != nil {
			return nil, err
		}
		if target.Role != user.RoleUser {
			return nil, fmt.Errorf("%w: plugins cannot ban or unban administrator accounts", account.ErrForbidden)
		}
		banned := true
		if len(args) > 2 && args[2] != nil {
			b, ok := args[2].(bool)
			if ok {
				banned = b
			}
		}
		if !banned && target.Status == user.StatusDeleted {
			return nil, fmt.Errorf("%w: plugins cannot reactivate deleted user accounts", account.ErrForbidden)
		}
		status := "active"
		if banned {
			status = "banned"
		}
		u, err := r.accounts.AdminUpdateUser(ctx, nil, uid, account.AdminUpdate{Status: &status}, account.RequestMeta{})
		if err != nil {
			return nil, err
		}
		return serializeUserDTO(u), nil

	default:
		return nil, fmt.Errorf("unknown users op %q", op)
	}
}

func (r *pluginRuntime) hostAuth(_ context.Context, args []any) (any, error) {
	op, _ := argAt(args, 0).(string)
	switch op {
	case "registerProvider":
		if err := r.perms.Check(PermAuthProvider); err != nil {
			return nil, err
		}
		spec, ok := asMap(argAt(args, 1))
		if !ok {
			return nil, errors.New("auth.registerProvider requires provider config object")
		}
		id := strings.TrimSpace(fmt.Sprint(spec["id"]))
		if id == "" || id == "<nil>" {
			return nil, errors.New("auth provider id is required")
		}
		name, _ := spec["name"].(string)
		if name == "" {
			name = id
		}
		icon := strings.TrimSpace(fmt.Sprint(spec["icon"]))
		if spec["icon"] == nil || icon == "<nil>" {
			icon = ""
		}
		authURL := ""
		if rawURL, has := spec["authUrl"].(string); has && strings.TrimSpace(rawURL) != "" {
			cleaned := strings.TrimSpace(rawURL)
			if !(strings.HasPrefix(cleaned, "/") && !strings.HasPrefix(cleaned, "//")) &&
				!strings.HasPrefix(cleaned, "https://") && !strings.HasPrefix(cleaned, "http://") {
				return nil, errors.New("auth provider authUrl must be a site path or http(s) URL")
			}
			authURL = cleaned
		}
		mode, _ := spec["mode"].(string)
		if mode == "" {
			mode = "credentials"
		}
		if mode != "credentials" && mode != "redirect" {
			return nil, fmt.Errorf("invalid auth provider mode %q", mode)
		}
		fnID := -1
		if len(args) > 2 && args[2] != nil {
			fnID = int(asFloat(args[2]))
		}
		r.setupMu.Lock()
		if r.collecting {
			r.authProviders = append(r.authProviders, authProviderBinding{
				Def: account.AuthProviderDef{
					ID:      id,
					Name:    name,
					Icon:    icon,
					AuthURL: authURL,
					Mode:    mode,
				},
				VerifyFn: fnID,
			})
		}
		r.setupMu.Unlock()
		return nil, nil

	case "registerPasswordVerifier":
		if err := r.perms.Check(PermAuthPassword); err != nil {
			return nil, err
		}
		algo := strings.TrimSpace(fmt.Sprint(argAt(args, 1)))
		if algo == "" || algo == "<nil>" {
			return nil, errors.New("password verifier algorithm name is required")
		}
		fnID := int(asFloat(argAt(args, 2)))
		r.setupMu.Lock()
		if r.collecting {
			r.verifiers = append(r.verifiers, passwordVerifierBinding{Algo: algo, FnID: fnID})
		}
		r.setupMu.Unlock()
		return nil, nil

	default:
		return nil, fmt.Errorf("unknown auth op %q", op)
	}
}

func (r *pluginRuntime) hostAudit(ctx context.Context, args []any) (any, error) {
	if r.db == nil {
		return nil, errors.New("database unavailable")
	}
	action, _ := argAt(args, 0).(string)
	targetType, _ := argAt(args, 1).(string)
	targetID, _ := argAt(args, 2).(string)
	if action == "" {
		return nil, errors.New("audit.record requires action")
	}
	entry := &audit.Entry{
		Action: "plugin." + r.id + "." + action,
		Meta:   map[string]any{},
	}
	if targetType != "" {
		entry = entry.Target(targetType, targetID)
	}
	if metaMap, ok := asMap(argAt(args, 3)); ok {
		for k, v := range metaMap {
			entry = entry.With(k, v)
		}
	}
	entry = entry.With("plugin", r.id)
	return nil, audit.Write(ctx, r.db, entry)
}
