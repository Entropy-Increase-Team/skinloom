package plugin

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	entplayer "github.com/Entropy-Increase-Team/skinloom/ent/player"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/audit"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/player"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/score"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/texture"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
)

func serializePlayerDTO(p *ent.Player) map[string]any {
	if p == nil {
		return nil
	}
	out := map[string]any{
		"id":        p.ID,
		"userId":    p.UserID,
		"name":      p.Name,
		"uuid":      p.UUID,
		"createdAt": p.CreatedAt.UTC().Format(time.RFC3339),
		"updatedAt": p.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if p.SkinID != nil {
		out["skinId"] = *p.SkinID
	} else {
		out["skinId"] = nil
	}
	if p.CapeID != nil {
		out["capeId"] = *p.CapeID
	} else {
		out["capeId"] = nil
	}
	return out
}

func serializeTextureDTO(t *ent.Texture) map[string]any {
	if t == nil {
		return nil
	}
	out := map[string]any{
		"id":         t.ID,
		"name":       t.Name,
		"type":       string(t.Type),
		"hash":       t.Hash,
		"size":       t.Size,
		"visibility": string(t.Visibility),
		"public":     t.Visibility == "public",
		"likes":      t.Likes,
		"createdAt":  t.CreatedAt.UTC().Format(time.RFC3339),
		"updatedAt":  t.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if t.UploaderID != nil {
		out["uploaderId"] = *t.UploaderID
	} else {
		out["uploaderId"] = nil
	}
	return out
}

func serializeClosetItemDTO(c *ent.ClosetItem) map[string]any {
	if c == nil {
		return nil
	}
	out := map[string]any{
		"id":        c.ID,
		"userId":    c.UserID,
		"textureId": c.TextureID,
		"name":      c.Name,
		"createdAt": c.CreatedAt.UTC().Format(time.RFC3339),
	}
	if c.Edges.Texture != nil {
		out["texture"] = serializeTextureDTO(c.Edges.Texture)
	}
	return out
}

func (r *pluginRuntime) currentPlayerPolicy() player.Policy {
	if r.siteSettings == nil {
		return player.Policy{Min: 3, Max: 16}
	}
	return player.PolicyFrom(r.siteSettings.Current())
}

func (r *pluginRuntime) currentTexturePolicy() texture.Policy {
	if r.siteSettings == nil {
		return texture.Policy{}
	}
	return texture.PolicyFrom(r.siteSettings.Current())
}

func (r *pluginRuntime) currentScoreRates() score.Rates {
	if r.siteSettings == nil {
		return score.Rates{}
	}
	return score.RatesFrom(r.siteSettings.Current())
}

func (r *pluginRuntime) hostPlayers(ctx context.Context, args []any) (any, error) {
	if r.players == nil {
		return nil, errors.New("player service unavailable")
	}
	op, _ := argAt(args, 0).(string)
	switch op {
	case "get":
		if err := r.perms.Check(PermPlayersRead); err != nil {
			return nil, err
		}
		pid := int64(asFloat(argAt(args, 1)))
		p, err := r.players.Get(ctx, pid)
		if ent.IsNotFound(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return serializePlayerDTO(p), nil

	case "findByName":
		if err := r.perms.Check(PermPlayersRead); err != nil {
			return nil, err
		}
		name, _ := argAt(args, 1).(string)
		if r.db == nil {
			return nil, errors.New("database unavailable")
		}
		p, err := r.db.Player.Query().Where(entplayer.NameLowerEQ(player.Key(strings.TrimSpace(name)))).Only(ctx)
		if ent.IsNotFound(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return serializePlayerDTO(p), nil

	case "findByUUID":
		if err := r.perms.Check(PermPlayersRead); err != nil {
			return nil, err
		}
		rawUUID, _ := argAt(args, 1).(string)
		p, err := r.players.FindByUUID(ctx, rawUUID)
		if errors.Is(err, player.ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return serializePlayerDTO(p), nil

	case "listAll":
		if err := r.perms.Check(PermPlayersRead); err != nil {
			return nil, err
		}
		afterID := int64(asFloat(argAt(args, 1)))
		limit := int(asFloat(argAt(args, 2)))
		if limit <= 0 {
			limit = 200
		}
		if limit > 500 {
			limit = 500
		}
		rows, err := r.players.ListAll(ctx, afterID, limit)
		if err != nil {
			return nil, err
		}
		items := make([]map[string]any, 0, len(rows))
		for _, p := range rows {
			items = append(items, serializePlayerDTO(p))
		}
		var nextAfterID int64
		if len(rows) == limit {
			nextAfterID = rows[len(rows)-1].ID
		}
		return map[string]any{"items": items, "nextAfterId": nextAfterID}, nil

	case "remapUUID":
		if err := r.perms.Check(PermPlayersUUIDWrite); err != nil {
			return nil, err
		}
		oldUUID, _ := argAt(args, 1).(string)
		newUUID, _ := argAt(args, 2).(string)
		rec := (&audit.Entry{
			Action: "plugin.player.remapUuid",
			Meta:   map[string]any{"plugin": r.id},
		}).Target("player", oldUUID)
		p, err := r.players.RemapUUID(ctx, oldUUID, newUUID, rec)
		if err != nil {
			return nil, err
		}
		return serializePlayerDTO(p), nil

	case "list":
		if err := r.perms.Check(PermPlayersRead); err != nil {
			return nil, err
		}
		uid := int64(asFloat(argAt(args, 1)))
		rows, err := r.players.List(ctx, uid)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]any, 0, len(rows))
		for _, p := range rows {
			out = append(out, serializePlayerDTO(p))
		}
		return out, nil

	case "create":
		if err := r.perms.Check(PermPlayersWrite); err != nil {
			return nil, err
		}
		uid := int64(asFloat(argAt(args, 1)))
		name, _ := argAt(args, 2).(string)
		form, _ := asMap(argAt(args, 3))
		var p *ent.Player
		var bal int64
		var err error
		if form != nil {
			p, bal, err = r.players.Create(ctx, uid, name, r.currentPlayerPolicy(), r.currentScoreRates(), form)
		} else {
			p, bal, err = r.players.Create(ctx, uid, name, r.currentPlayerPolicy(), r.currentScoreRates())
		}
		if err != nil {
			return nil, err
		}
		res := serializePlayerDTO(p)
		res["balance"] = bal
		return res, nil

	case "rename":
		if err := r.perms.Check(PermPlayersWrite); err != nil {
			return nil, err
		}
		uid := int64(asFloat(argAt(args, 1)))
		pid := int64(asFloat(argAt(args, 2)))
		name, _ := argAt(args, 3).(string)
		p, err := r.players.Rename(ctx, uid, pid, name, r.currentPlayerPolicy())
		if err != nil {
			return nil, err
		}
		return serializePlayerDTO(p), nil

	case "setTextures":
		if err := r.perms.Check(PermPlayersWrite); err != nil {
			return nil, err
		}
		uid := int64(asFloat(argAt(args, 1)))
		pid := int64(asFloat(argAt(args, 2)))
		spec, _ := asMap(argAt(args, 3))
		var skinPtr, capePtr *int64
		if spec != nil {
			if raw, has := spec["skin"]; has {
				if raw == nil {
					v := int64(0)
					skinPtr = &v
				} else {
					v := int64(asFloat(raw))
					skinPtr = &v
				}
			}
			if raw, has := spec["cape"]; has {
				if raw == nil {
					v := int64(0)
					capePtr = &v
				} else {
					v := int64(asFloat(raw))
					capePtr = &v
				}
			}
		}
		p, err := r.players.Wear(ctx, uid, pid, skinPtr, capePtr, nil)
		if err != nil {
			return nil, err
		}
		return serializePlayerDTO(p), nil

	case "delete":
		if err := r.perms.Check(PermPlayersWrite); err != nil {
			return nil, err
		}
		uid := int64(asFloat(argAt(args, 1)))
		pid := int64(asFloat(argAt(args, 2)))
		refund := true
		if b, ok := argAt(args, 3).(bool); ok {
			refund = b
		}
		bal, err := r.players.Delete(ctx, uid, pid, refund, nil)
		if err != nil {
			return nil, err
		}
		return map[string]any{"deleted": true, "balance": bal}, nil

	default:
		return nil, fmt.Errorf("unknown players op %q", op)
	}
}

func (r *pluginRuntime) hostTextures(ctx context.Context, args []any) (any, error) {
	if r.textures == nil {
		return nil, errors.New("texture service unavailable")
	}
	op, _ := argAt(args, 0).(string)
	switch op {
	case "get":
		if err := r.perms.Check(PermTexturesRead); err != nil {
			return nil, err
		}
		tid := int64(asFloat(argAt(args, 1)))
		tex, err := r.textures.Get(ctx, tid, texture.Viewer{Admin: true})
		if ent.IsNotFound(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return serializeTextureDTO(tex), nil

	case "list":
		if err := r.perms.Check(PermTexturesRead); err != nil {
			return nil, err
		}
		opts, _ := asMap(argAt(args, 1))
		f := texture.Filter{}
		if opts != nil {
			f.Category, _ = opts["category"].(string)
			f.Query, _ = opts["q"].(string)
			f.UploaderID = int64(asFloat(opts["uploaderId"]))
			f.Sort, _ = opts["sort"].(string)
			f.Cursor, _ = opts["cursor"].(string)
			f.Limit = int(asFloat(opts["limit"]))
		}
		rows, nextCursor, err := r.textures.List(ctx, f, texture.Viewer{Admin: true})
		if err != nil {
			return nil, err
		}
		items := make([]map[string]any, 0, len(rows))
		for _, t := range rows {
			items = append(items, serializeTextureDTO(t))
		}
		return map[string]any{"items": items, "nextCursor": nextCursor}, nil

	case "readBytes":
		if err := r.perms.Check(PermTexturesRead); err != nil {
			return nil, err
		}
		tid := int64(asFloat(argAt(args, 1)))
		tex, err := r.textures.Get(ctx, tid, texture.Viewer{Admin: true})
		if err != nil {
			return nil, err
		}
		return r.textures.ReadFile(ctx, tex.Hash)

	case "create":
		if err := r.perms.Check(PermTexturesWrite); err != nil {
			return nil, err
		}
		spec, ok := asMap(argAt(args, 1))
		if !ok {
			return nil, errors.New("textures.create requires input object")
		}
		uploaderID := int64(asFloat(spec["uploaderId"]))
		name, _ := spec["name"].(string)
		typ, _ := spec["type"].(string)
		pub := true
		if b, has := spec["public"].(bool); has {
			pub = b
		}
		data := argToBytes(argAt(args, 2))
		if len(data) == 0 {
			data = argToBytes(spec["data"])
		}
		if len(data) == 0 {
			if b64, has := spec["dataBase64"].(string); has && b64 != "" {
				decoded, err := base64.StdEncoding.DecodeString(b64)
				if err != nil {
					return nil, err
				}
				data = decoded
			}
		}
		form, _ := spec["form"].(map[string]any)
		res, err := r.textures.Upload(ctx, uploaderID, texture.UploadInput{
			Name:   name,
			Type:   typ,
			Public: pub,
			Data:   data,
			Form:   form,
		}, r.currentTexturePolicy(), r.currentScoreRates())
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"texture":   serializeTextureDTO(res.Texture),
			"duplicate": res.Duplicate,
			"cost":      res.Cost,
			"balance":   res.Balance,
		}, nil

	case "update":
		if err := r.perms.Check(PermTexturesWrite); err != nil {
			return nil, err
		}
		tid := int64(asFloat(argAt(args, 1)))
		patch, ok := asMap(argAt(args, 2))
		if !ok {
			return nil, errors.New("textures.update requires patch object")
		}
		edit := texture.Edit{}
		if v, has := patch["name"].(string); has {
			edit.Name = &v
		}
		if v, has := patch["type"].(string); has {
			edit.Type = &v
		}
		if v, has := patch["public"].(bool); has {
			edit.Public = &v
		}
		tex, err := r.textures.Update(ctx, texture.Viewer{Admin: true}, tid, edit, r.currentTexturePolicy(), r.currentScoreRates(), nil)
		if err != nil {
			return nil, err
		}
		return serializeTextureDTO(tex), nil

	case "delete":
		if err := r.perms.Check(PermTexturesWrite); err != nil {
			return nil, err
		}
		tid := int64(asFloat(argAt(args, 1)))
		refund := false
		if b, ok := argAt(args, 2).(bool); ok {
			refund = b
		}
		if err := r.textures.Delete(ctx, texture.Viewer{Admin: true}, tid, refund, nil); err != nil {
			return nil, err
		}
		return map[string]any{"deleted": true}, nil

	default:
		return nil, fmt.Errorf("unknown textures op %q", op)
	}
}

func (r *pluginRuntime) hostCloset(ctx context.Context, args []any) (any, error) {
	if r.textures == nil {
		return nil, errors.New("texture service unavailable")
	}
	if err := r.perms.Check(PermClosetWrite); err != nil {
		return nil, err
	}
	op, _ := argAt(args, 0).(string)
	uid := int64(asFloat(argAt(args, 1)))
	switch op {
	case "list":
		opts, _ := asMap(argAt(args, 2))
		f := texture.ClosetFilter{}
		if opts != nil {
			f.Category, _ = opts["category"].(string)
			f.Query, _ = opts["q"].(string)
			f.Before = int64(asFloat(opts["before"]))
			f.Limit = int(asFloat(opts["limit"]))
		}
		rows, nextBefore, err := r.textures.Closet(ctx, uid, f)
		if err != nil {
			return nil, err
		}
		items := make([]map[string]any, 0, len(rows))
		for _, item := range rows {
			items = append(items, serializeClosetItemDTO(item))
		}
		return map[string]any{"items": items, "nextBefore": nextBefore}, nil

	case "add":
		tid := int64(asFloat(argAt(args, 2)))
		name, _ := argAt(args, 3).(string)
		item, bal, err := r.textures.Favorite(ctx, texture.Viewer{UserID: uid}, tid, name, r.currentScoreRates())
		if err != nil {
			return nil, err
		}
		return map[string]any{"item": serializeClosetItemDTO(item), "balance": bal}, nil

	case "rename":
		tid := int64(asFloat(argAt(args, 2)))
		name, _ := argAt(args, 3).(string)
		item, err := r.textures.RenameClosetItem(ctx, uid, tid, name)
		if err != nil {
			return nil, err
		}
		return serializeClosetItemDTO(item), nil

	case "remove":
		tid := int64(asFloat(argAt(args, 2)))
		refund := true
		if b, ok := argAt(args, 3).(bool); ok {
			refund = b
		}
		bal, err := r.textures.Unfavorite(ctx, uid, tid, refund, nil)
		if err != nil {
			return nil, err
		}
		return map[string]any{"removed": true, "balance": bal}, nil

	default:
		return nil, fmt.Errorf("unknown closet op %q", op)
	}
}

func (r *pluginRuntime) hostScore(ctx context.Context, args []any) (any, error) {
	if r.db == nil {
		return nil, errors.New("database unavailable")
	}
	op, _ := argAt(args, 0).(string)
	uid := int64(asFloat(argAt(args, 1)))
	switch op {
	case "balance":
		if !r.perms.Has(PermScoreWrite) && !r.perms.Has(PermUsersRead) {
			return nil, fmt.Errorf("%w: %s or %s", ErrPermissionDenied, PermScoreWrite, PermUsersRead)
		}
		u, err := r.db.User.Query().Where(user.ID(uid)).Only(ctx)
		if err != nil {
			return nil, err
		}
		return u.Score, nil

	case "apply":
		if err := r.perms.Check(PermScoreWrite); err != nil {
			return nil, err
		}
		delta := int64(asFloat(argAt(args, 2)))
		reason, _ := argAt(args, 3).(string)
		if reason == "" {
			reason = "plugin." + r.id
		}
		refType, _ := argAt(args, 4).(string)
		refID, _ := argAt(args, 5).(string)
		if refType == "" {
			refType = "plugin"
		}
		if refID == "" {
			refID = r.id
		}
		var balance int64
		err := db.WithTx(ctx, r.db, func(tx *ent.Client) error {
			if err := score.Lock(ctx, tx, uid); err != nil {
				return err
			}
			var err error
			balance, err = score.Apply(ctx, tx, score.Change{
				UserID:  uid,
				Delta:   delta,
				Reason:  reason,
				RefType: refType,
				RefID:   refID,
				Hooks:   r.bus,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		return balance, nil

	default:
		return nil, fmt.Errorf("unknown score op %q", op)
	}
}
