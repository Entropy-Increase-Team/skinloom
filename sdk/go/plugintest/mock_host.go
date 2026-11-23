package plugintest

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pluginv1 "github.com/Entropy-Increase-Team/skinloom/api/proto/plugin/v1"
	plugin "github.com/Entropy-Increase-Team/skinloom/sdk/go"
)

// SentNotification records a host Notify.Send call.
type SentNotification struct {
	UserIDs  []int64
	Title    string
	Markdown string
}

// SentAnnouncement records a host Notify.Announce call.
type SentAnnouncement struct {
	ID       int64
	Audience string
	Title    string
	Markdown string
}

// RecordedAudit records a host Audit.Record call.
type RecordedAudit struct {
	Action     string
	TargetType string
	TargetID   string
	Meta       map[string]any
}

// EmittedEvent records a host Events.Emit call.
type EmittedEvent struct {
	LocalName string
	Payload   json.RawMessage
}

// CapturedLog records a host Log call.
type CapturedLog struct {
	Level   string
	Message string
}

type mockKVEntry struct {
	val       json.RawMessage
	expiresAt time.Time
}

type mockCacheEntry struct {
	val       json.RawMessage
	expiresAt time.Time
}

// MockHost is an in-memory implementation of pluginv1.HostServiceClient used by
// Harness so native Go plugins can be unit-tested with plain `go test`.
type MockHost struct {
	mu sync.RWMutex

	enforcePerms bool
	perms        map[string]bool

	SiteName        string
	SiteURL         string
	SiteDescription string
	SiteLocales     []string
	SitePublic      map[string]any
	Settings        map[string]any
	Locales          map[string]map[string]string
	YggdrasilPubKey  string
	YggdrasilPrivKey string

	Users        map[int64]*plugin.User
	Players      map[int64]*plugin.Player
	Textures     map[int64]*plugin.Texture
	TextureBytes map[int64][]byte
	Closet       map[int64][]plugin.ClosetItem

	KVStore    map[string]mockKVEntry
	DocsStore  map[string][]map[string]any
	FilesStore map[string][]byte
	CacheStore map[string]mockCacheEntry

	FetchHandler    func(ctx context.Context, req plugin.FetchRequest) (*plugin.FetchResponse, error)
	ServiceHandler  func(ctx context.Context, targetPluginID, serviceName string, args []json.RawMessage) (any, error)
	DiscoverHandler func(ctx context.Context, serviceName string) ([]string, error)

	Notifications []SentNotification
	Announcements []SentAnnouncement
	Mails         []plugin.MailOptions
	Audits        []RecordedAudit
	Events        []EmittedEvent
	Logs          []CapturedLog

	nextID int64
}

// NewMockHost creates an initialized MockHost with sensible defaults.
func NewMockHost() *MockHost {
	return &MockHost{
		SiteName:        "SkinLoom Test",
		SiteURL:         "https://skinloom.test",
		SiteDescription: "SkinLoom Test Instance",
		SiteLocales:     []string{"zh-CN", "en"},
		SitePublic:      map[string]any{"site.name": "SkinLoom Test"},
		Settings:        map[string]any{},
		Locales:         map[string]map[string]string{},
		Users:           map[int64]*plugin.User{},
		Players:         map[int64]*plugin.Player{},
		Textures:        map[int64]*plugin.Texture{},
		TextureBytes:    map[int64][]byte{},
		Closet:          map[int64][]plugin.ClosetItem{},
		KVStore:         map[string]mockKVEntry{},
		DocsStore:       map[string][]map[string]any{},
		FilesStore:      map[string][]byte{},
		CacheStore:      map[string]mockCacheEntry{},
		nextID:          100,
	}
}

func (m *MockHost) setPermissions(enforce bool, perms []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enforcePerms = enforce
	m.perms = make(map[string]bool, len(perms))
	for _, p := range perms {
		m.perms[p] = true
	}
}

func (m *MockHost) checkPermLocked(perm string) error {
	if m.enforcePerms && !m.perms[perm] {
		return status.Errorf(codes.PermissionDenied, "permission %q not granted", perm)
	}
	return nil
}

func (m *MockHost) checkPerm(perm string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.checkPermLocked(perm)
}

func (m *MockHost) allocIDLocked() int64 {
	m.nextID++
	return m.nextID
}

func decodeArgs(req *pluginv1.HostOpRequest) ([]any, error) {
	if len(req.GetArgsJson()) == 0 {
		return nil, nil
	}
	var out []any
	if err := json.Unmarshal(req.GetArgsJson(), &out); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid args_json: %v", err)
	}
	return out, nil
}

func encodeResp(val any) (*pluginv1.HostOpResponse, error) {
	if b, ok := val.([]byte); ok {
		return &pluginv1.HostOpResponse{BinaryData: b}, nil
	}
	if val == nil {
		return &pluginv1.HostOpResponse{}, nil
	}
	raw, err := json.Marshal(val)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "marshal mock response: %v", err)
	}
	return &pluginv1.HostOpResponse{ResultJson: raw}, nil
}

func asNum(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	default:
		return 0
	}
}

func strArg(args []any, idx int) string {
	if idx >= len(args) || args[idx] == nil {
		return ""
	}
	s, _ := args[idx].(string)
	return s
}

// Client returns a pluginv1.HostServiceClient backed by this MockHost.
func (m *MockHost) Client() pluginv1.HostServiceClient {
	return &mockHostClient{m: m}
}

type mockHostClient struct {
	m *MockHost
}

func (c *mockHostClient) KV(_ context.Context, req *pluginv1.HostOpRequest, _ ...grpc.CallOption) (*pluginv1.HostOpResponse, error) {
	m := c.m
	if err := m.checkPerm("storage.kv"); err != nil {
		return nil, err
	}
	args, err := decodeArgs(req)
	if err != nil {
		return nil, err
	}
	key := strArg(args, 0)
	now := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()
	switch req.GetOp() {
	case "get":
		it, ok := m.KVStore[key]
		if !ok || (!it.expiresAt.IsZero() && !it.expiresAt.After(now)) {
			delete(m.KVStore, key)
			return &pluginv1.HostOpResponse{}, nil
		}
		return &pluginv1.HostOpResponse{ResultJson: append([]byte(nil), it.val...)}, nil
	case "set":
		var raw json.RawMessage
		if len(args) > 1 {
			raw, _ = json.Marshal(args[1])
		}
		var exp time.Time
		if len(args) > 2 && asNum(args[2]) > 0 {
			exp = now.Add(time.Duration(asNum(args[2]) * float64(time.Second)))
		}
		m.KVStore[key] = mockKVEntry{val: raw, expiresAt: exp}
		return &pluginv1.HostOpResponse{}, nil
	case "delete":
		_, existed := m.KVStore[key]
		delete(m.KVStore, key)
		return encodeResp(existed)
	case "list":
		limit := 100
		if len(args) > 1 && int(asNum(args[1])) > 0 {
			limit = int(asNum(args[1]))
		}
		var keys []string
		for k, it := range m.KVStore {
			if !it.expiresAt.IsZero() && !it.expiresAt.After(now) {
				delete(m.KVStore, k)
				continue
			}
			if strings.HasPrefix(k, key) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		out := make([]map[string]any, 0, len(keys))
		for _, k := range keys {
			if len(out) >= limit {
				break
			}
			var decoded any
			_ = json.Unmarshal(m.KVStore[k].val, &decoded)
			out = append(out, map[string]any{"key": k, "value": decoded})
		}
		return encodeResp(out)
	case "incr":
		delta := 1.0
		if len(args) > 1 && args[1] != nil {
			delta = asNum(args[1])
		}
		var cur float64
		if it, ok := m.KVStore[key]; ok && (it.expiresAt.IsZero() || it.expiresAt.After(now)) {
			_ = json.Unmarshal(it.val, &cur)
		}
		cur += delta
		raw, _ := json.Marshal(cur)
		m.KVStore[key] = mockKVEntry{val: raw}
		return encodeResp(cur)
	default:
		return nil, status.Errorf(codes.InvalidArgument, "unknown kv op %q", req.GetOp())
	}
}

func (c *mockHostClient) Docs(_ context.Context, req *pluginv1.HostOpRequest, _ ...grpc.CallOption) (*pluginv1.HostOpResponse, error) {
	m := c.m
	if err := m.checkPerm("storage.docs"); err != nil {
		return nil, err
	}
	args, err := decodeArgs(req)
	if err != nil {
		return nil, err
	}
	coll := strArg(args, 0)
	m.mu.Lock()
	defer m.mu.Unlock()

	matches := func(doc, filter map[string]any) bool {
		for k, v := range filter {
			if fmt.Sprint(doc[k]) != fmt.Sprint(v) {
				return false
			}
		}
		return true
	}

	switch req.GetOp() {
	case "insertOne":
		doc, _ := args[1].(map[string]any)
		cp := map[string]any{}
		for k, v := range doc {
			cp[k] = v
		}
		if _, has := cp["id"]; !has {
			cp["id"] = fmt.Sprintf("doc_%d", m.allocIDLocked())
		}
		m.DocsStore[coll] = append(m.DocsStore[coll], cp)
		return encodeResp(cp)
	case "findOne":
		var filter map[string]any
		if len(args) > 1 {
			filter, _ = args[1].(map[string]any)
		}
		for _, doc := range m.DocsStore[coll] {
			if matches(doc, filter) {
				return encodeResp(doc)
			}
		}
		return &pluginv1.HostOpResponse{}, nil
	case "find":
		var filter map[string]any
		if len(args) > 1 {
			filter, _ = args[1].(map[string]any)
		}
		var items []map[string]any
		for _, doc := range m.DocsStore[coll] {
			if matches(doc, filter) {
				items = append(items, doc)
			}
		}
		return encodeResp(map[string]any{"items": items, "nextCursor": ""})
	case "updateOne":
		filter, _ := args[1].(map[string]any)
		patch, _ := args[2].(map[string]any)
		matched := false
		for _, doc := range m.DocsStore[coll] {
			if matches(doc, filter) {
				for k, v := range patch {
					doc[k] = v
				}
				matched = true
				break
			}
		}
		return encodeResp(map[string]any{"matched": matched, "modified": matched})
	case "updateMany":
		filter, _ := args[1].(map[string]any)
		patch, _ := args[2].(map[string]any)
		var mod int64
		for _, doc := range m.DocsStore[coll] {
			if matches(doc, filter) {
				for k, v := range patch {
					doc[k] = v
				}
				mod++
			}
		}
		return encodeResp(map[string]any{"matched": mod, "modified": mod})
	case "deleteOne":
		filter, _ := args[1].(map[string]any)
		docs := m.DocsStore[coll]
		for i, doc := range docs {
			if matches(doc, filter) {
				m.DocsStore[coll] = append(docs[:i], docs[i+1:]...)
				return encodeResp(map[string]any{"deleted": true})
			}
		}
		return encodeResp(map[string]any{"deleted": false})
	case "count":
		var filter map[string]any
		if len(args) > 1 {
			filter, _ = args[1].(map[string]any)
		}
		var n int64
		for _, doc := range m.DocsStore[coll] {
			if matches(doc, filter) {
				n++
			}
		}
		return encodeResp(n)
	default:
		return nil, status.Errorf(codes.InvalidArgument, "unknown docs op %q", req.GetOp())
	}
}

func (c *mockHostClient) FilesPut(_ context.Context, req *pluginv1.FilesPutRequest, _ ...grpc.CallOption) (*pluginv1.FilesPutResponse, error) {
	m := c.m
	if err := m.checkPerm("storage.files"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.FilesStore[req.GetKey()] = append([]byte(nil), req.GetData()...)
	m.mu.Unlock()
	return &pluginv1.FilesPutResponse{Url: "/plugins-assets/mock/_files/" + req.GetKey()}, nil
}

func (c *mockHostClient) FilesGet(_ context.Context, req *pluginv1.FilesKeyRequest, _ ...grpc.CallOption) (*pluginv1.FilesGetResponse, error) {
	m := c.m
	if err := m.checkPerm("storage.files"); err != nil {
		return nil, err
	}
	m.mu.RLock()
	data, ok := m.FilesStore[req.GetKey()]
	m.mu.RUnlock()
	if !ok {
		return &pluginv1.FilesGetResponse{Found: false}, nil
	}
	return &pluginv1.FilesGetResponse{Found: true, Data: append([]byte(nil), data...)}, nil
}

func (c *mockHostClient) FilesDelete(_ context.Context, req *pluginv1.FilesKeyRequest, _ ...grpc.CallOption) (*pluginv1.FilesDeleteResponse, error) {
	m := c.m
	if err := m.checkPerm("storage.files"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	_, ok := m.FilesStore[req.GetKey()]
	delete(m.FilesStore, req.GetKey())
	m.mu.Unlock()
	return &pluginv1.FilesDeleteResponse{Deleted: ok}, nil
}

func (c *mockHostClient) FilesURL(_ context.Context, req *pluginv1.FilesKeyRequest, _ ...grpc.CallOption) (*pluginv1.FilesURLResponse, error) {
	m := c.m
	if err := m.checkPerm("storage.files"); err != nil {
		return nil, err
	}
	return &pluginv1.FilesURLResponse{Url: "/plugins-assets/mock/_files/" + req.GetKey()}, nil
}

func (c *mockHostClient) Fetch(ctx context.Context, req *pluginv1.FetchRequest, _ ...grpc.CallOption) (*pluginv1.FetchResponse, error) {
	m := c.m
	if err := m.checkPerm("http.fetch"); err != nil {
		return nil, err
	}
	m.mu.RLock()
	h := m.FetchHandler
	m.mu.RUnlock()
	if h == nil {
		return nil, status.Error(codes.Unimplemented, "FetchHandler not configured in plugintest")
	}
	resp, err := h(ctx, plugin.FetchRequest{
		URL:       req.GetUrl(),
		Method:    req.GetMethod(),
		Headers:   req.GetHeaders(),
		Body:      req.GetBody(),
		TimeoutMs: int(req.GetTimeoutMs()),
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pluginv1.FetchResponse{
		Ok:         resp.OK,
		Status:     int32(resp.Status),
		StatusText: resp.StatusText,
		Headers:    resp.Headers,
		Url:        resp.URL,
		Body:       resp.Body,
	}, nil
}

func (c *mockHostClient) Settings(_ context.Context, req *pluginv1.HostOpRequest, _ ...grpc.CallOption) (*pluginv1.HostOpResponse, error) {
	m := c.m
	args, err := decodeArgs(req)
	if err != nil {
		return nil, err
	}
	switch req.GetOp() {
	case "get":
		m.mu.RLock()
		defer m.mu.RUnlock()
		return encodeResp(m.Settings[strArg(args, 0)])
	case "all":
		m.mu.RLock()
		defer m.mu.RUnlock()
		return encodeResp(m.Settings)
	case "patch":
		if len(args) == 0 {
			return nil, status.Error(codes.InvalidArgument, "settings patch argument is required")
		}
		patch, ok := args[0].(map[string]any)
		if !ok {
			return nil, status.Error(codes.InvalidArgument, "settings patch must be an object")
		}
		m.mu.Lock()
		if m.Settings == nil {
			m.Settings = map[string]any{}
		}
		for k, v := range patch {
			m.Settings[k] = v
		}
		out := make(map[string]any, len(m.Settings))
		for k, v := range m.Settings {
			out[k] = v
		}
		m.mu.Unlock()
		return encodeResp(out)
	default:
		return nil, status.Errorf(codes.InvalidArgument, "unknown settings op %q", req.GetOp())
	}
}

func (c *mockHostClient) Site(_ context.Context, req *pluginv1.HostOpRequest, _ ...grpc.CallOption) (*pluginv1.HostOpResponse, error) {
	m := c.m
	m.mu.RLock()
	defer m.mu.RUnlock()
	switch req.GetOp() {
	case "name":
		return encodeResp(m.SiteName)
	case "url":
		return encodeResp(m.SiteURL)
	case "description":
		return encodeResp(m.SiteDescription)
	case "locales":
		return encodeResp(m.SiteLocales)
	case "public":
		return encodeResp(m.SitePublic)
	default:
		return nil, status.Errorf(codes.InvalidArgument, "unknown site op %q", req.GetOp())
	}
}

func (c *mockHostClient) Users(_ context.Context, req *pluginv1.HostOpRequest, _ ...grpc.CallOption) (*pluginv1.HostOpResponse, error) {
	m := c.m
	args, err := decodeArgs(req)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	switch req.GetOp() {
	case "get":
		if err := m.checkPermLocked("users.read"); err != nil {
			return nil, err
		}
		return encodeResp(m.Users[int64(asNum(args[0]))])
	case "find":
		if err := m.checkPermLocked("users.read"); err != nil {
			return nil, err
		}
		email := strings.ToLower(strArg(args, 0))
		for _, u := range m.Users {
			if strings.ToLower(u.Email) == email {
				return encodeResp(u)
			}
		}
		return &pluginv1.HostOpResponse{}, nil
	case "list":
		if err := m.checkPermLocked("users.read"); err != nil {
			return nil, err
		}
		items := make([]*plugin.User, 0, len(m.Users))
		for _, u := range m.Users {
			items = append(items, u)
		}
		sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
		return encodeResp(map[string]any{"items": items, "nextBefore": 0})
	case "update":
		if err := m.checkPermLocked("users.write"); err != nil {
			return nil, err
		}
		uid := int64(asNum(args[0]))
		u := m.Users[uid]
		if u == nil {
			return nil, status.Error(codes.NotFound, "user not found")
		}
		if patch, ok := args[1].(map[string]any); ok {
			if n, ok := patch["nickname"].(string); ok {
				u.Nickname = n
			}
			if v, ok := patch["emailVerified"].(bool); ok {
				u.EmailVerified = v
			}
			if loc, ok := patch["locale"].(string); ok {
				u.Locale = loc
			}
		}
		return encodeResp(u)
	case "ban":
		if err := m.checkPermLocked("users.ban"); err != nil {
			return nil, err
		}
		uid := int64(asNum(args[0]))
		u := m.Users[uid]
		if u == nil {
			return nil, status.Error(codes.NotFound, "user not found")
		}
		banned := true
		if len(args) > 1 {
			if b, ok := args[1].(bool); ok {
				banned = b
			}
		}
		if banned {
			u.Status = "banned"
		} else {
			u.Status = "active"
		}
		return encodeResp(u)
	default:
		return nil, status.Errorf(codes.InvalidArgument, "unknown users op %q", req.GetOp())
	}
}

func (c *mockHostClient) Players(_ context.Context, req *pluginv1.HostOpRequest, _ ...grpc.CallOption) (*pluginv1.HostOpResponse, error) {
	m := c.m
	args, err := decodeArgs(req)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	switch req.GetOp() {
	case "get":
		if err := m.checkPermLocked("players.read"); err != nil {
			return nil, err
		}
		return encodeResp(m.Players[int64(asNum(args[0]))])
	case "findByName":
		if err := m.checkPermLocked("players.read"); err != nil {
			return nil, err
		}
		name := strings.ToLower(strArg(args, 0))
		for _, p := range m.Players {
			if strings.ToLower(p.Name) == name {
				return encodeResp(p)
			}
		}
		return &pluginv1.HostOpResponse{}, nil
	case "findByUUID":
		if err := m.checkPermLocked("players.read"); err != nil {
			return nil, err
		}
		target := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(strArg(args, 0)), "-", ""))
		for _, p := range m.Players {
			if strings.ToLower(strings.ReplaceAll(p.UUID, "-", "")) == target {
				return encodeResp(p)
			}
		}
		return &pluginv1.HostOpResponse{}, nil
	case "listAll":
		if err := m.checkPermLocked("players.read"); err != nil {
			return nil, err
		}
		afterID := int64(asNum(args[0]))
		limit := int(asNum(args[1]))
		if limit <= 0 {
			limit = 200
		}
		if limit > 500 {
			limit = 500
		}
		all := make([]*plugin.Player, 0, len(m.Players))
		for _, p := range m.Players {
			if p.ID > afterID {
				all = append(all, p)
			}
		}
		sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
		var nextAfterID int64
		if len(all) > limit {
			all = all[:limit]
			nextAfterID = all[len(all)-1].ID
		} else if len(all) == limit && limit > 0 {
			nextAfterID = all[len(all)-1].ID
		}
		return encodeResp(map[string]any{"items": all, "nextAfterId": nextAfterID})
	case "remapUUID":
		if err := m.checkPermLocked("players.uuid.write"); err != nil {
			return nil, err
		}
		oldNorm := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(strArg(args, 0)), "-", ""))
		newNorm := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(strArg(args, 1)), "-", ""))
		if len(oldNorm) != 32 || len(newNorm) != 32 {
			return nil, status.Error(codes.InvalidArgument, "invalid UUID")
		}
		var target *plugin.Player
		for _, p := range m.Players {
			if strings.ToLower(strings.ReplaceAll(p.UUID, "-", "")) == oldNorm {
				target = p
				break
			}
		}
		if target == nil {
			return nil, status.Error(codes.NotFound, "player not found")
		}
		if oldNorm != newNorm {
			for _, p := range m.Players {
				if p.ID != target.ID && strings.ToLower(strings.ReplaceAll(p.UUID, "-", "")) == newNorm {
					return nil, status.Error(codes.AlreadyExists, "UUID already in use")
				}
			}
			target.UUID = newNorm
		}
		m.Audits = append(m.Audits, RecordedAudit{
			Action:     "plugin.mock.player.remapUuid",
			TargetType: "player",
			TargetID:   oldNorm,
			Meta:       map[string]any{"uuid": []string{oldNorm, newNorm}},
		})
		return encodeResp(target)
	case "list":
		if err := m.checkPermLocked("players.read"); err != nil {
			return nil, err
		}
		uid := int64(asNum(args[0]))
		var out []*plugin.Player
		for _, p := range m.Players {
			if p.UserID == uid {
				out = append(out, p)
			}
		}
		return encodeResp(out)
	case "create":
		if err := m.checkPermLocked("players.write"); err != nil {
			return nil, err
		}
		uid := int64(asNum(args[0]))
		name := strArg(args, 1)
		id := m.allocIDLocked()
		p := &plugin.Player{ID: id, UserID: uid, Name: name, UUID: fmt.Sprintf("mock-uuid-%d", id)}
		m.Players[id] = p
		return encodeResp(p)
	case "rename":
		if err := m.checkPermLocked("players.write"); err != nil {
			return nil, err
		}
		pid := int64(asNum(args[1]))
		p := m.Players[pid]
		if p == nil {
			return nil, status.Error(codes.NotFound, "player not found")
		}
		p.Name = strArg(args, 2)
		return encodeResp(p)
	case "setTextures":
		if err := m.checkPermLocked("players.write"); err != nil {
			return nil, err
		}
		pid := int64(asNum(args[1]))
		p := m.Players[pid]
		if p == nil {
			return nil, status.Error(codes.NotFound, "player not found")
		}
		if spec, ok := args[2].(map[string]any); ok {
			if raw, has := spec["skin"]; has {
				if raw == nil {
					p.SkinID = nil
				} else {
					v := int64(asNum(raw))
					p.SkinID = &v
				}
			}
			if raw, has := spec["cape"]; has {
				if raw == nil {
					p.CapeID = nil
				} else {
					v := int64(asNum(raw))
					p.CapeID = &v
				}
			}
		}
		return encodeResp(p)
	case "delete":
		if err := m.checkPermLocked("players.write"); err != nil {
			return nil, err
		}
		pid := int64(asNum(args[1]))
		delete(m.Players, pid)
		return encodeResp(map[string]any{"deleted": true, "balance": 0})
	default:
		return nil, status.Errorf(codes.InvalidArgument, "unknown players op %q", req.GetOp())
	}
}

func (c *mockHostClient) Textures(_ context.Context, req *pluginv1.HostOpRequest, _ ...grpc.CallOption) (*pluginv1.HostOpResponse, error) {
	m := c.m
	args, err := decodeArgs(req)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	switch req.GetOp() {
	case "get":
		if err := m.checkPermLocked("textures.read"); err != nil {
			return nil, err
		}
		return encodeResp(m.Textures[int64(asNum(args[0]))])
	case "list":
		if err := m.checkPermLocked("textures.read"); err != nil {
			return nil, err
		}
		items := make([]*plugin.Texture, 0, len(m.Textures))
		for _, t := range m.Textures {
			items = append(items, t)
		}
		return encodeResp(map[string]any{"items": items, "nextCursor": ""})
	case "readBytes":
		if err := m.checkPermLocked("textures.read"); err != nil {
			return nil, err
		}
		data, ok := m.TextureBytes[int64(asNum(args[0]))]
		if !ok {
			return nil, status.Error(codes.NotFound, "texture bytes not found")
		}
		return &pluginv1.HostOpResponse{BinaryData: append([]byte(nil), data...)}, nil
	case "create":
		if err := m.checkPermLocked("textures.write"); err != nil {
			return nil, err
		}
		spec, _ := args[0].(map[string]any)
		id := m.allocIDLocked()
		uid := int64(asNum(spec["uploaderId"]))
		tex := &plugin.Texture{
			ID:         id,
			Name:       fmt.Sprint(spec["name"]),
			Type:       fmt.Sprint(spec["type"]),
			Public:     spec["public"] == true,
			UploaderID: &uid,
			Size:       int64(len(req.GetBinaryData())),
		}
		m.Textures[id] = tex
		m.TextureBytes[id] = append([]byte(nil), req.GetBinaryData()...)
		return encodeResp(map[string]any{"texture": tex, "duplicate": false, "cost": 0, "balance": 0})
	case "update":
		if err := m.checkPermLocked("textures.write"); err != nil {
			return nil, err
		}
		tid := int64(asNum(args[0]))
		tex := m.Textures[tid]
		if tex == nil {
			return nil, status.Error(codes.NotFound, "texture not found")
		}
		if patch, ok := args[1].(map[string]any); ok {
			if n, ok := patch["name"].(string); ok {
				tex.Name = n
			}
			if p, ok := patch["public"].(bool); ok {
				tex.Public = p
			}
		}
		return encodeResp(tex)
	case "delete":
		if err := m.checkPermLocked("textures.write"); err != nil {
			return nil, err
		}
		tid := int64(asNum(args[0]))
		delete(m.Textures, tid)
		delete(m.TextureBytes, tid)
		return encodeResp(map[string]any{"deleted": true})
	default:
		return nil, status.Errorf(codes.InvalidArgument, "unknown textures op %q", req.GetOp())
	}
}

func (c *mockHostClient) Closet(_ context.Context, req *pluginv1.HostOpRequest, _ ...grpc.CallOption) (*pluginv1.HostOpResponse, error) {
	m := c.m
	if err := m.checkPerm("closet.write"); err != nil {
		return nil, err
	}
	args, err := decodeArgs(req)
	if err != nil {
		return nil, err
	}
	uid := int64(asNum(args[0]))
	m.mu.Lock()
	defer m.mu.Unlock()
	switch req.GetOp() {
	case "list":
		return encodeResp(map[string]any{"items": m.Closet[uid], "nextBefore": 0})
	case "add":
		tid := int64(asNum(args[1]))
		name := strArg(args, 2)
		it := plugin.ClosetItem{ID: m.allocIDLocked(), UserID: uid, TextureID: tid, Name: name}
		m.Closet[uid] = append(m.Closet[uid], it)
		return encodeResp(map[string]any{"item": it, "balance": 0})
	case "rename":
		tid := int64(asNum(args[1]))
		name := strArg(args, 2)
		for i := range m.Closet[uid] {
			if m.Closet[uid][i].TextureID == tid {
				m.Closet[uid][i].Name = name
				return encodeResp(m.Closet[uid][i])
			}
		}
		return nil, status.Error(codes.NotFound, "closet item not found")
	case "remove":
		tid := int64(asNum(args[1]))
		items := m.Closet[uid]
		for i := range items {
			if items[i].TextureID == tid {
				m.Closet[uid] = append(items[:i], items[i+1:]...)
				break
			}
		}
		return encodeResp(map[string]any{"removed": true, "balance": 0})
	default:
		return nil, status.Errorf(codes.InvalidArgument, "unknown closet op %q", req.GetOp())
	}
}

func (c *mockHostClient) Score(_ context.Context, req *pluginv1.HostOpRequest, _ ...grpc.CallOption) (*pluginv1.HostOpResponse, error) {
	m := c.m
	args, err := decodeArgs(req)
	if err != nil {
		return nil, err
	}
	uid := int64(asNum(args[0]))
	m.mu.Lock()
	defer m.mu.Unlock()
	u := m.Users[uid]
	if u == nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}
	switch req.GetOp() {
	case "balance":
		if m.enforcePerms && !m.perms["score.write"] && !m.perms["users.read"] {
			return nil, status.Error(codes.PermissionDenied, "requires score.write or users.read")
		}
		return encodeResp(u.Score)
	case "apply":
		if err := m.checkPermLocked("score.write"); err != nil {
			return nil, err
		}
		delta := int64(asNum(args[1]))
		u.Score += delta
		return encodeResp(u.Score)
	default:
		return nil, status.Errorf(codes.InvalidArgument, "unknown score op %q", req.GetOp())
	}
}

func (c *mockHostClient) Notify(_ context.Context, req *pluginv1.HostOpRequest, _ ...grpc.CallOption) (*pluginv1.HostOpResponse, error) {
	m := c.m
	if err := m.checkPerm("notify.send"); err != nil {
		return nil, err
	}
	args, err := decodeArgs(req)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	switch req.GetOp() {
	case "send":
		var ids []int64
		if arr, ok := args[0].([]any); ok {
			for _, it := range arr {
				ids = append(ids, int64(asNum(it)))
			}
		} else {
			ids = []int64{int64(asNum(args[0]))}
		}
		m.Notifications = append(m.Notifications, SentNotification{
			UserIDs:  ids,
			Title:    strArg(args, 1),
			Markdown: strArg(args, 2),
		})
		return encodeResp(map[string]any{"sent": len(ids)})
	case "announce":
		id := m.allocIDLocked()
		ann := SentAnnouncement{
			ID:       id,
			Audience: strArg(args, 0),
			Title:    strArg(args, 1),
			Markdown: strArg(args, 2),
		}
		m.Announcements = append(m.Announcements, ann)
		return encodeResp(map[string]any{"id": id, "audience": ann.Audience})
	default:
		return nil, status.Errorf(codes.InvalidArgument, "unknown notify op %q", req.GetOp())
	}
}

func (c *mockHostClient) Mail(_ context.Context, req *pluginv1.MailSendRequest, _ ...grpc.CallOption) (*pluginv1.HostOpResponse, error) {
	m := c.m
	if err := m.checkPerm("mail.send"); err != nil {
		return nil, err
	}
	var opts plugin.MailOptions
	if err := json.Unmarshal(req.GetOptionsJson(), &opts); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid mail options: %v", err)
	}
	m.mu.Lock()
	m.Mails = append(m.Mails, opts)
	m.mu.Unlock()
	return encodeResp(map[string]any{"sent": true})
}

func (c *mockHostClient) Yggdrasil(_ context.Context, req *pluginv1.HostOpRequest, _ ...grpc.CallOption) (*pluginv1.HostOpResponse, error) {
	m := c.m
	if req.GetOp() == "setSigningKey" {
		if err := m.checkPerm("yggdrasil.key.write"); err != nil {
			return nil, err
		}
		args, err := decodeArgs(req)
		if err != nil {
			return nil, err
		}
		pemText := strings.TrimSpace(strArg(args, 0))
		block, _ := pem.Decode([]byte(pemText))
		if block == nil {
			return nil, status.Error(codes.InvalidArgument, "not PEM")
		}
		var rk *rsa.PrivateKey
		if parsed, pErr := x509.ParsePKCS8PrivateKey(block.Bytes); pErr == nil {
			var ok bool
			rk, ok = parsed.(*rsa.PrivateKey)
			if !ok {
				return nil, status.Error(codes.InvalidArgument, "not RSA")
			}
		} else if parsed1, p1Err := x509.ParsePKCS1PrivateKey(block.Bytes); p1Err == nil {
			rk = parsed1
		} else {
			return nil, status.Error(codes.InvalidArgument, "unsupported RSA private key")
		}
		if rk.N.BitLen() < 4096 {
			return nil, status.Errorf(codes.InvalidArgument, "RSA key too small: %d bits", rk.N.BitLen())
		}
		pubDER, err := x509.MarshalPKIXPublicKey(&rk.PublicKey)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		pubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}))
		m.mu.Lock()
		m.YggdrasilPrivKey = pemText
		m.YggdrasilPubKey = pubPEM
		m.Audits = append(m.Audits, RecordedAudit{
			Action:     "plugin.mock.yggdrasil.setSigningKey",
			TargetType: "yggdrasil",
			TargetID:   "signing_key",
		})
		m.mu.Unlock()
		return encodeResp(map[string]any{"updated": true})
	}
	if err := m.checkPerm("yggdrasil.extend"); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	switch req.GetOp() {
	case "publicKey":
		return encodeResp(m.YggdrasilPubKey)
	default:
		return encodeResp(map[string]any{})
	}
}

func (c *mockHostClient) Audit(_ context.Context, req *pluginv1.AuditRecordRequest, _ ...grpc.CallOption) (*pluginv1.Empty, error) {
	m := c.m
	var meta map[string]any
	if len(req.GetMetaJson()) > 0 {
		_ = json.Unmarshal(req.GetMetaJson(), &meta)
	}
	m.mu.Lock()
	m.Audits = append(m.Audits, RecordedAudit{
		Action:     req.GetAction(),
		TargetType: req.GetTargetType(),
		TargetID:   req.GetTargetId(),
		Meta:       meta,
	})
	m.mu.Unlock()
	return &pluginv1.Empty{}, nil
}

func (c *mockHostClient) EmitEvent(_ context.Context, req *pluginv1.EmitEventRequest, _ ...grpc.CallOption) (*pluginv1.Empty, error) {
	m := c.m
	m.mu.Lock()
	m.Events = append(m.Events, EmittedEvent{
		LocalName: req.GetLocalName(),
		Payload:   append(json.RawMessage(nil), req.GetPayloadJson()...),
	})
	m.mu.Unlock()
	return &pluginv1.Empty{}, nil
}

func (c *mockHostClient) CallPluginService(ctx context.Context, req *pluginv1.CallPluginServiceRequest, _ ...grpc.CallOption) (*pluginv1.HostOpResponse, error) {
	m := c.m
	m.mu.RLock()
	h := m.ServiceHandler
	m.mu.RUnlock()
	if h == nil {
		return nil, status.Error(codes.Unimplemented, "ServiceHandler not configured in plugintest")
	}
	var args []json.RawMessage
	if len(req.GetArgsJson()) > 0 {
		_ = json.Unmarshal(req.GetArgsJson(), &args)
	}
	res, err := h(ctx, req.GetTargetPluginId(), req.GetServiceName(), args)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return encodeResp(res)
}

func (c *mockHostClient) DiscoverPluginServices(ctx context.Context, req *pluginv1.DiscoverPluginServicesRequest, _ ...grpc.CallOption) (*pluginv1.DiscoverPluginServicesResponse, error) {
	m := c.m
	m.mu.RLock()
	h := m.DiscoverHandler
	m.mu.RUnlock()
	if h == nil {
		return &pluginv1.DiscoverPluginServicesResponse{}, nil
	}
	ids, err := h(ctx, req.GetServiceName())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pluginv1.DiscoverPluginServicesResponse{PluginIds: ids}, nil
}

func (c *mockHostClient) Log(_ context.Context, req *pluginv1.LogRequest, _ ...grpc.CallOption) (*pluginv1.Empty, error) {
	m := c.m
	m.mu.Lock()
	m.Logs = append(m.Logs, CapturedLog{Level: req.GetLevel(), Message: req.GetMessage()})
	m.mu.Unlock()
	return &pluginv1.Empty{}, nil
}

func (c *mockHostClient) Cache(_ context.Context, req *pluginv1.HostOpRequest, _ ...grpc.CallOption) (*pluginv1.HostOpResponse, error) {
	m := c.m
	args, err := decodeArgs(req)
	if err != nil {
		return nil, err
	}
	key := strArg(args, 0)
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	switch req.GetOp() {
	case "get":
		it, ok := m.CacheStore[key]
		if !ok || (!it.expiresAt.IsZero() && !it.expiresAt.After(now)) {
			delete(m.CacheStore, key)
			return &pluginv1.HostOpResponse{}, nil
		}
		return &pluginv1.HostOpResponse{ResultJson: append([]byte(nil), it.val...)}, nil
	case "set":
		raw, _ := json.Marshal(args[1])
		var exp time.Time
		if len(args) > 2 && asNum(args[2]) > 0 {
			exp = now.Add(time.Duration(asNum(args[2]) * float64(time.Second)))
		}
		m.CacheStore[key] = mockCacheEntry{val: raw, expiresAt: exp}
		return &pluginv1.HostOpResponse{}, nil
	case "delete":
		_, ok := m.CacheStore[key]
		delete(m.CacheStore, key)
		return encodeResp(ok)
	default:
		return nil, status.Errorf(codes.InvalidArgument, "unknown cache op %q", req.GetOp())
	}
}

func (c *mockHostClient) I18N(_ context.Context, req *pluginv1.HostOpRequest, _ ...grpc.CallOption) (*pluginv1.HostOpResponse, error) {
	m := c.m
	args, err := decodeArgs(req)
	if err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if req.GetOp() == "locales" {
		out := make([]string, 0, len(m.Locales))
		for k := range m.Locales {
			out = append(out, k)
		}
		sort.Strings(out)
		return encodeResp(out)
	}
	key := strArg(args, 0)
	locale := strArg(args, 2)
	if locale == "" {
		locale = req.GetLocale()
	}
	if tbl, ok := m.Locales[locale]; ok {
		if v, exists := tbl[key]; exists {
			return encodeResp(v)
		}
	}
	return encodeResp(key)
}

var _ pluginv1.HostServiceClient = (*mockHostClient)(nil)
