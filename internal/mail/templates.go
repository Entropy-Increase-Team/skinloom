package mail

import (
	"bytes"
	htmltemplate "html/template"
	"strings"
	texttemplate "text/template"
)

// Kinds of mail SkinLoom sends.
const (
	KindVerifyEmail   = "verify_email"
	KindResetPassword = "reset_password"
	KindEmailChanged  = "email_changed"
	KindTest          = "test"
)

// Data fills a template.
type Data struct {
	Site     string // localized site name
	Nickname string
	Link     string
	NewEmail string // email_changed
}

type template struct {
	subject, greeting, body, action, footer string
}

// Texts per kind and locale. {{.Site}} etc. are filled in; the HTML version escapes.
var texts = map[string]map[string]template{
	KindVerifyEmail: {
		"zh-CN": {
			subject:  "验证你在 {{.Site}} 的邮箱",
			greeting: "{{.Nickname}}，你好：",
			body:     "请点击下面的按钮验证你的邮箱。链接 24 小时内有效，只能使用一次。",
			action:   "验证邮箱",
			footer:   "如果这不是你本人的操作，忽略这封邮件即可。",
		},
		"en": {
			subject:  "Verify your email for {{.Site}}",
			greeting: "Hi {{.Nickname}},",
			body:     "Please confirm your email address with the button below. The link works once and expires in 24 hours.",
			action:   "Verify email",
			footer:   "If you did not sign up, you can ignore this email.",
		},
	},
	KindResetPassword: {
		"zh-CN": {
			subject:  "重置你在 {{.Site}} 的密码",
			greeting: "{{.Nickname}}，你好：",
			body:     "我们收到了重置密码的请求。请在 30 分钟内点击下面的按钮设置新密码，链接只能使用一次。重置后，所有设备上的登录都会失效。",
			action:   "设置新密码",
			footer:   "如果你没有申请重置密码，忽略这封邮件即可，你的密码不会改变。",
		},
		"en": {
			subject:  "Reset your {{.Site}} password",
			greeting: "Hi {{.Nickname}},",
			body:     "We received a request to reset your password. Use the button below within 30 minutes to choose a new one; the link works once. Afterwards you will be signed out everywhere.",
			action:   "Choose a new password",
			footer:   "If you did not ask for this, ignore this email; your password stays the same.",
		},
	},
	KindEmailChanged: {
		"zh-CN": {
			subject:  "你在 {{.Site}} 的账号邮箱已更改",
			greeting: "{{.Nickname}}，你好：",
			body:     "你账号的邮箱刚刚改成了 {{.NewEmail}}，之后的通知都会发到新邮箱，其它设备上的登录已经失效。",
			footer:   "如果这不是你本人的操作，你的账号可能已经被他人控制，请立即联系站点管理员。",
		},
		"en": {
			subject:  "Your {{.Site}} account email was changed",
			greeting: "Hi {{.Nickname}},",
			body:     "The email address of your account was just changed to {{.NewEmail}}. Mail now goes there, and your other devices were signed out.",
			footer:   "If you did not do this, someone else may control your account: contact the site administrator right away.",
		},
	},
	KindTest: {
		"zh-CN": {
			subject: "{{.Site}} 测试邮件",
			body:    "这是一封测试邮件。收到它，说明 SkinLoom 的邮件设置可以正常工作。",
		},
		"en": {
			subject: "{{.Site}} test email",
			body:    "This is a test email. If you can read it, SkinLoom's mail settings work.",
		},
	},
}

var htmlLayout = htmltemplate.Must(htmltemplate.New("mail").Parse(`<!doctype html>
<html><body style="margin:0;padding:24px;background:#f4f4f5;font-family:-apple-system,'Segoe UI','PingFang SC','Microsoft YaHei',sans-serif;color:#18181b">
<div style="max-width:520px;margin:0 auto;background:#fff;border-radius:12px;padding:32px">
<h2 style="margin:0 0 24px;font-size:18px">{{.Site}}</h2>
{{if .Greeting}}<p>{{.Greeting}}</p>{{end}}
<p style="line-height:1.6">{{.Body}}</p>
{{if .Link}}<p style="margin:28px 0"><a href="{{.Link}}" style="background:#2563eb;color:#fff;text-decoration:none;padding:12px 20px;border-radius:8px;display:inline-block">{{.Action}}</a></p>
<p style="font-size:12px;color:#71717a;word-break:break-all">{{.Link}}</p>{{end}}
{{if .Footer}}<p style="font-size:13px;color:#71717a;line-height:1.6">{{.Footer}}</p>{{end}}
</div></body></html>`))

// Render builds a message for kind in locale (falling back to zh-CN).
func Render(kind, locale, to string, d Data) Message {
	byLocale := texts[kind]
	t, ok := byLocale[locale]
	if !ok {
		t = byLocale["zh-CN"]
	}
	fill := func(s string) string {
		if s == "" {
			return ""
		}
		var b strings.Builder
		_ = texttemplate.Must(texttemplate.New("").Parse(s)).Execute(&b, d)
		return b.String()
	}
	greeting, body, action, footer := fill(t.greeting), fill(t.body), fill(t.action), fill(t.footer)

	var text strings.Builder
	for _, s := range []string{greeting, body, d.Link, footer, d.Site} {
		if s != "" {
			text.WriteString(s + "\n\n")
		}
	}
	var html bytes.Buffer
	_ = htmlLayout.Execute(&html, map[string]string{
		"Site": d.Site, "Greeting": greeting, "Body": body, "Link": d.Link, "Action": action, "Footer": footer,
	})
	return Message{To: to, Subject: fill(t.subject), Text: strings.TrimSpace(text.String()) + "\n", HTML: html.String()}
}
