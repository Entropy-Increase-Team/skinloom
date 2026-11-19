package plugin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Entropy-Increase-Team/skinloom/internal/modules/cdn"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/mail"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/notify"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/yggdrasil"
)

type captchaBinding struct {
	ID        string
	Name      string
	FnID      int
	HandlerID string
}

type cdnBinding struct {
	ID        string
	Name      string
	FnID      int
	HandlerID string
}

type yggMetaBinding struct {
	FnID      int
	HandlerID string
	Static    *yggdrasil.MetaExtension
}

func (r *pluginRuntime) hostNotify(ctx context.Context, args []any) (any, error) {
	if r.notify == nil {
		return nil, errors.New("notify service unavailable")
	}
	if err := r.perms.Check(PermNotifySend); err != nil {
		return nil, err
	}
	op, _ := argAt(args, 0).(string)
	switch op {
	case "send":
		title, _ := argAt(args, 2).(string)
		markdown, _ := argAt(args, 3).(string)
		var userIDs []int64
		switch raw := argAt(args, 1).(type) {
		case []any:
			for _, item := range raw {
				uid := int64(asFloat(item))
				if uid <= 0 {
					return nil, errors.New("notify.send userIds must contain positive user IDs")
				}
				userIDs = append(userIDs, uid)
			}
		case float64, int, int64:
			uid := int64(asFloat(raw))
			if uid <= 0 {
				return nil, errors.New("notify.send requires positive userId")
			}
			userIDs = append(userIDs, uid)
		default:
			return nil, errors.New("notify.send expects userId or userId[]")
		}
		var sent int
		for _, uid := range userIDs {
			if _, err := r.notify.Send(ctx, uid, title, markdown, nil); err != nil {
				return nil, err
			}
			sent++
		}
		return map[string]any{"sent": sent}, nil

	case "announce":
		audience, _ := argAt(args, 1).(string)
		if audience == "" {
			audience = notify.AudienceAll
		}
		title, _ := argAt(args, 2).(string)
		markdown, _ := argAt(args, 3).(string)
		ann, err := r.notify.Announce(ctx, nil, audience, title, markdown, nil)
		if err != nil {
			return nil, err
		}
		return map[string]any{"id": ann.ID, "audience": ann.Audience}, nil

	default:
		return nil, fmt.Errorf("unknown notify op %q", op)
	}
}

func (r *pluginRuntime) hostMail(ctx context.Context, args []any) (any, error) {
	if r.mailer == nil {
		return nil, errors.New("mail service unavailable")
	}
	if err := r.perms.Check(PermMailSend); err != nil {
		return nil, err
	}
	if r.siteSettings == nil {
		return nil, errors.New("settings service unavailable")
	}
	spec, ok := asMap(argAt(args, 0))
	if !ok {
		return nil, errors.New("mail.send requires options object")
	}
	to, _ := spec["to"].(string)
	to = strings.TrimSpace(to)
	if to == "" {
		return nil, errors.New("mail.send requires non-empty recipient 'to'")
	}

	var msg mail.Message
	if tpl, has := spec["template"].(string); has && tpl != "" {
		if !mail.HasTemplate(tpl) {
			return nil, fmt.Errorf("unknown mail template %q", tpl)
		}
		locale, _ := spec["locale"].(string)
		if locale == "" {
			locale = requestLocaleFrom(ctx)
		}
		dataMap, _ := asMap(spec["data"])
		d := mail.Data{Site: r.siteSettings.Current().Localized("site.name", locale)}
		if dataMap != nil {
			if s, ok := dataMap["site"].(string); ok && s != "" {
				d.Site = s
			}
			d.Nickname, _ = dataMap["nickname"].(string)
			d.Link, _ = dataMap["link"].(string)
			d.NewEmail, _ = dataMap["newEmail"].(string)
		}
		msg = mail.Render(tpl, locale, to, d)
		if subj, has := spec["subject"].(string); has && subj != "" {
			msg.Subject = subj
		}
	} else {
		subj, _ := spec["subject"].(string)
		textBody, _ := spec["text"].(string)
		htmlBody, _ := spec["html"].(string)
		msg = mail.Message{
			To:      to,
			Subject: subj,
			Text:    textBody,
			HTML:    htmlBody,
		}
	}
	if err := r.mailer.Send(ctx, mail.ConfigFrom(r.siteSettings.Current()), msg); err != nil {
		return nil, err
	}
	return map[string]any{"sent": true}, nil
}

func (r *pluginRuntime) hostCaptcha(_ context.Context, args []any) (any, error) {
	op, _ := argAt(args, 0).(string)
	switch op {
	case "registerProvider":
		if err := r.perms.Check(PermCaptchaProvider); err != nil {
			return nil, err
		}
		spec, ok := asMap(argAt(args, 1))
		if !ok {
			return nil, errors.New("captcha.registerProvider requires provider config")
		}
		id := strings.TrimSpace(fmt.Sprint(spec["id"]))
		if id == "" || id == "<nil>" {
			return nil, errors.New("captcha provider id is required")
		}
		name, _ := spec["name"].(string)
		if name == "" {
			name = id
		}
		fnID := int(asFloat(argAt(args, 2)))
		r.setupMu.Lock()
		if r.collecting {
			r.captchas = append(r.captchas, captchaBinding{ID: id, Name: name, FnID: fnID})
		}
		r.setupMu.Unlock()
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown captcha op %q", op)
	}
}

func (r *pluginRuntime) hostCDN(_ context.Context, args []any) (any, error) {
	op, _ := argAt(args, 0).(string)
	switch op {
	case "registerPurger":
		if err := r.perms.Check(PermCDNProvider); err != nil {
			return nil, err
		}
		spec, ok := asMap(argAt(args, 1))
		if !ok {
			return nil, errors.New("cdn.registerPurger requires config object")
		}
		id := strings.TrimSpace(fmt.Sprint(spec["id"]))
		if id == "" || id == "<nil>" {
			return nil, errors.New("cdn provider id is required")
		}
		name, _ := spec["name"].(string)
		if name == "" {
			name = id
		}
		fnID := int(asFloat(argAt(args, 2)))
		r.setupMu.Lock()
		if r.collecting {
			r.cdns = append(r.cdns, cdnBinding{ID: id, Name: name, FnID: fnID})
		}
		r.setupMu.Unlock()
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown cdn op %q", op)
	}
}

func (r *pluginRuntime) hostYggdrasil(ctx context.Context, args []any) (any, error) {
	if err := r.perms.Check(PermYggdrasilExtend); err != nil {
		return nil, err
	}
	if r.yggdrasil == nil {
		return nil, errors.New("yggdrasil service unavailable")
	}
	op, _ := argAt(args, 0).(string)
	switch op {
	case "publicKey":
		pemText, _, err := r.yggdrasil.PublicKey(ctx)
		return pemText, err
	case "lookupName":
		name, _ := argAt(args, 1).(string)
		return r.yggdrasil.LookupName(ctx, name)
	case "lookupProfile":
		uuid, _ := argAt(args, 1).(string)
		base, _ := argAt(args, 2).(string)
		if base == "" && r.siteSettings != nil {
			base = r.siteSettings.Current().String("site.url")
		}
		return r.yggdrasil.ProfileByUUID(ctx, uuid, strings.TrimRight(base, "/"), false)
	case "extendMeta":
		extMap, ok := asMap(argAt(args, 1))
		if !ok {
			return nil, errors.New("yggdrasil.extendMeta requires object")
		}
		ext := &yggdrasil.MetaExtension{}
		if rawDomains, ok := extMap["skinDomains"].([]any); ok {
			for _, d := range rawDomains {
				if s, ok := d.(string); ok && s != "" {
					ext.SkinDomains = append(ext.SkinDomains, s)
				}
			}
		}
		if rawMeta, ok := asMap(extMap["meta"]); ok {
			ext.Meta = cloneMap(rawMeta)
		}
		r.setupMu.Lock()
		if r.collecting {
			r.yggMetas = append(r.yggMetas, yggMetaBinding{FnID: -1, Static: ext})
		}
		r.setupMu.Unlock()
		return nil, nil
	case "registerMetaProvider":
		fnID := int(asFloat(argAt(args, 1)))
		r.setupMu.Lock()
		if r.collecting {
			r.yggMetas = append(r.yggMetas, yggMetaBinding{FnID: fnID})
		}
		r.setupMu.Unlock()
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown yggdrasil op %q", op)
	}
}

type pluginCDNPurger struct {
	rt        *pluginRuntime
	id        string
	handlerID string
}

func (p pluginCDNPurger) Purge(ctx context.Context, urls []string) error {
	_, err := p.rt.invokeExtension(ctx, "cdn", p.id, p.handlerID, urls)
	return err
}

var _ cdn.Purger = (*pluginCDNPurger)(nil)
