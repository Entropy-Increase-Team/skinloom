package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"google.golang.org/grpc"

	pluginv1 "github.com/Entropy-Increase-Team/skinloom/api/proto/plugin/v1"
)

type hostOpRPC func(ctx context.Context, in *pluginv1.HostOpRequest, opts ...grpc.CallOption) (*pluginv1.HostOpResponse, error)

func callHostOp(ctx context.Context, rpc hostOpRPC, op string, args []any, dst any) error {
	return callHostOpLocale(ctx, rpc, op, args, "", nil, dst)
}

func callHostOpLocale(
	ctx context.Context,
	rpc hostOpRPC,
	op string,
	args []any,
	locale string,
	binaryData []byte,
	dst any,
) error {
	if rpc == nil {
		return errors.New("host service unavailable")
	}
	var rawArgs []byte
	if len(args) > 0 {
		var err error
		rawArgs, err = json.Marshal(args)
		if err != nil {
			return err
		}
	}
	resp, err := rpc(ctx, &pluginv1.HostOpRequest{
		Op:         op,
		ArgsJson:   rawArgs,
		Locale:     locale,
		BinaryData: binaryData,
	})
	if err != nil {
		return err
	}
	if dst == nil {
		return nil
	}
	if bPtr, ok := dst.(*[]byte); ok {
		*bPtr = append([]byte(nil), resp.GetBinaryData()...)
		return nil
	}
	if len(resp.GetResultJson()) == 0 {
		return nil
	}
	return json.Unmarshal(resp.GetResultJson(), dst)
}

// --- Domain DTOs ---

type User struct {
	ID              int64   `json:"id"`
	Email           string  `json:"email"`
	Nickname        string  `json:"nickname"`
	Role            string  `json:"role"`
	Status          string  `json:"status"`
	Score           int64   `json:"score"`
	Locale          string  `json:"locale"`
	EmailVerified   bool    `json:"emailVerified"`
	AvatarTextureID *int64  `json:"avatarTextureId"`
	CreatedAt       string  `json:"createdAt"`
	LastLoginAt     *string `json:"lastLoginAt"`
}

type UserFilter struct {
	Query  string `json:"q,omitempty"`
	Role   string `json:"role,omitempty"`
	Status string `json:"status,omitempty"`
	Before int64  `json:"before,omitempty"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type UserList struct {
	Items      []User `json:"items"`
	NextBefore int64  `json:"nextBefore"`
}

type UserPatch struct {
	Nickname      *string `json:"nickname,omitempty"`
	EmailVerified *bool   `json:"emailVerified,omitempty"`
	Locale        *string `json:"locale,omitempty"`
}

type Player struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"userId"`
	Name      string `json:"name"`
	UUID      string `json:"uuid"`
	SkinID    *int64 `json:"skinId"`
	CapeID    *int64 `json:"capeId"`
	Balance   int64  `json:"balance,omitempty"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type PlayerPage struct {
	Items       []Player `json:"items"`
	NextAfterID int64    `json:"nextAfterId"`
}

type Texture struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Hash       string `json:"hash"`
	Size       int64  `json:"size"`
	Visibility string `json:"visibility"`
	Public     bool   `json:"public"`
	Likes      int    `json:"likes"`
	UploaderID *int64 `json:"uploaderId"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

type TextureFilter struct {
	Category   string `json:"category,omitempty"`
	Query      string `json:"q,omitempty"`
	UploaderID int64  `json:"uploaderId,omitempty"`
	Sort       string `json:"sort,omitempty"`
	Cursor     string `json:"cursor,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

type TextureList struct {
	Items      []Texture `json:"items"`
	NextCursor string    `json:"nextCursor"`
}

type TextureCreateInput struct {
	UploaderID int64          `json:"uploaderId"`
	Name       string         `json:"name"`
	Type       string         `json:"type"` // "steve" | "alex" | "cape"
	Public     bool           `json:"public"`
	Data       []byte         `json:"-"`
	Form       map[string]any `json:"form,omitempty"`
}

type TextureCreateResult struct {
	Texture   Texture `json:"texture"`
	Duplicate bool    `json:"duplicate"`
	Cost      int64   `json:"cost"`
	Balance   int64   `json:"balance"`
}

type TexturePatch struct {
	Name   *string `json:"name,omitempty"`
	Type   *string `json:"type,omitempty"`
	Public *bool   `json:"public,omitempty"`
}

type ClosetItem struct {
	ID        int64    `json:"id"`
	UserID    int64    `json:"userId"`
	TextureID int64    `json:"textureId"`
	Name      string   `json:"name"`
	CreatedAt string   `json:"createdAt"`
	Texture   *Texture `json:"texture,omitempty"`
}

type ClosetFilter struct {
	Category string `json:"category,omitempty"`
	Query    string `json:"q,omitempty"`
	Before   int64  `json:"before,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

type ClosetList struct {
	Items      []ClosetItem `json:"items"`
	NextBefore int64        `json:"nextBefore"`
}

type KVEntry struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

type FindOpts struct {
	Limit  int            `json:"limit,omitempty"`
	Cursor string         `json:"cursor,omitempty"`
	Sort   map[string]int `json:"sort,omitempty"`
}

type DocsFindResult struct {
	Items      []map[string]any `json:"items"`
	NextCursor string           `json:"nextCursor"`
}

type FetchRequest struct {
	URL       string
	Method    string
	Headers   map[string]string
	Body      []byte
	TimeoutMs int
}

type FetchResponse struct {
	OK         bool
	Status     int
	StatusText string
	Headers    map[string]string
	URL        string
	Body       []byte
}

type MailOptions struct {
	To       string         `json:"to"`
	Subject  string         `json:"subject,omitempty"`
	Text     string         `json:"text,omitempty"`
	HTML     string         `json:"html,omitempty"`
	Template string         `json:"template,omitempty"`
	Locale   string         `json:"locale,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
}

// --- Strongly-Typed Host Clients ---

type UsersClient struct{ host pluginv1.HostServiceClient }

func (c *UsersClient) Get(ctx context.Context, userID int64) (*User, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	var out *User
	if err := callHostOp(ctx, c.host.Users, "get", []any{userID}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *UsersClient) FindByEmail(ctx context.Context, email string) (*User, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	var out *User
	if err := callHostOp(ctx, c.host.Users, "find", []any{email}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *UsersClient) List(ctx context.Context, filter UserFilter) (*UserList, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	var out UserList
	if err := callHostOp(ctx, c.host.Users, "list", []any{filter}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *UsersClient) Update(ctx context.Context, userID int64, patch UserPatch) (*User, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	var out *User
	if err := callHostOp(ctx, c.host.Users, "update", []any{userID, patch}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *UsersClient) Ban(ctx context.Context, userID int64, banned bool) (*User, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	var out *User
	if err := callHostOp(ctx, c.host.Users, "ban", []any{userID, banned}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

type PlayersClient struct{ host pluginv1.HostServiceClient }

func (c *PlayersClient) Get(ctx context.Context, playerID int64) (*Player, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	var out *Player
	if err := callHostOp(ctx, c.host.Players, "get", []any{playerID}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *PlayersClient) FindByName(ctx context.Context, name string) (*Player, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	var out *Player
	if err := callHostOp(ctx, c.host.Players, "findByName", []any{name}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *PlayersClient) FindByUUID(ctx context.Context, uuid string) (*Player, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	var out *Player
	if err := callHostOp(ctx, c.host.Players, "findByUUID", []any{uuid}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *PlayersClient) ListAll(ctx context.Context, afterID int64, limit int) (*PlayerPage, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	var out PlayerPage
	if err := callHostOp(ctx, c.host.Players, "listAll", []any{afterID, limit}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RemapUUID changes the UUID of the player matching oldUUID to newUUID.
// Requires the high-risk "players.uuid.write" permission.
func (c *PlayersClient) RemapUUID(ctx context.Context, oldUUID, newUUID string) (*Player, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	var out *Player
	if err := callHostOp(ctx, c.host.Players, "remapUUID", []any{oldUUID, newUUID}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *PlayersClient) List(ctx context.Context, userID int64) ([]Player, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	var out []Player
	if err := callHostOp(ctx, c.host.Players, "list", []any{userID}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *PlayersClient) Create(ctx context.Context, userID int64, name string, form map[string]any) (*Player, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	args := []any{userID, name}
	if form != nil {
		args = append(args, form)
	}
	var out *Player
	if err := callHostOp(ctx, c.host.Players, "create", args, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *PlayersClient) Rename(ctx context.Context, userID, playerID int64, name string) (*Player, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	var out *Player
	if err := callHostOp(ctx, c.host.Players, "rename", []any{userID, playerID, name}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *PlayersClient) SetTextures(ctx context.Context, userID, playerID int64, skinID, capeID *int64) (*Player, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	spec := map[string]any{}
	if skinID != nil {
		if *skinID <= 0 {
			spec["skin"] = nil
		} else {
			spec["skin"] = *skinID
		}
	}
	if capeID != nil {
		if *capeID <= 0 {
			spec["cape"] = nil
		} else {
			spec["cape"] = *capeID
		}
	}
	var out *Player
	if err := callHostOp(ctx, c.host.Players, "setTextures", []any{userID, playerID, spec}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *PlayersClient) Delete(ctx context.Context, userID, playerID int64, refund bool) (int64, error) {
	if c.host == nil {
		return 0, errors.New("host service unavailable")
	}
	var out struct {
		Balance int64 `json:"balance"`
	}
	if err := callHostOp(ctx, c.host.Players, "delete", []any{userID, playerID, refund}, &out); err != nil {
		return 0, err
	}
	return out.Balance, nil
}

type TexturesClient struct{ host pluginv1.HostServiceClient }

func (c *TexturesClient) Get(ctx context.Context, textureID int64) (*Texture, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	var out *Texture
	if err := callHostOp(ctx, c.host.Textures, "get", []any{textureID}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *TexturesClient) List(ctx context.Context, filter TextureFilter) (*TextureList, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	var out TextureList
	if err := callHostOp(ctx, c.host.Textures, "list", []any{filter}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *TexturesClient) ReadBytes(ctx context.Context, textureID int64) ([]byte, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	var data []byte
	if err := callHostOp(ctx, c.host.Textures, "readBytes", []any{textureID}, &data); err != nil {
		return nil, err
	}
	return data, nil
}

func (c *TexturesClient) Create(ctx context.Context, in TextureCreateInput) (*TextureCreateResult, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	spec := map[string]any{
		"uploaderId": in.UploaderID,
		"name":       in.Name,
		"type":       in.Type,
		"public":     in.Public,
	}
	if in.Form != nil {
		spec["form"] = in.Form
	}
	var out TextureCreateResult
	if err := callHostOpLocale(ctx, c.host.Textures, "create", []any{spec}, "", in.Data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *TexturesClient) Update(ctx context.Context, textureID int64, patch TexturePatch) (*Texture, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	var out *Texture
	if err := callHostOp(ctx, c.host.Textures, "update", []any{textureID, patch}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *TexturesClient) Delete(ctx context.Context, textureID int64, refund bool) error {
	if c.host == nil {
		return errors.New("host service unavailable")
	}
	return callHostOp(ctx, c.host.Textures, "delete", []any{textureID, refund}, nil)
}

type ClosetClient struct{ host pluginv1.HostServiceClient }

func (c *ClosetClient) List(ctx context.Context, userID int64, filter ClosetFilter) (*ClosetList, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	var out ClosetList
	if err := callHostOp(ctx, c.host.Closet, "list", []any{userID, filter}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *ClosetClient) Add(ctx context.Context, userID, textureID int64, name string) (*ClosetItem, int64, error) {
	if c.host == nil {
		return nil, 0, errors.New("host service unavailable")
	}
	var out struct {
		Item    *ClosetItem `json:"item"`
		Balance int64       `json:"balance"`
	}
	if err := callHostOp(ctx, c.host.Closet, "add", []any{userID, textureID, name}, &out); err != nil {
		return nil, 0, err
	}
	return out.Item, out.Balance, nil
}

func (c *ClosetClient) Rename(ctx context.Context, userID, textureID int64, name string) (*ClosetItem, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	var out *ClosetItem
	if err := callHostOp(ctx, c.host.Closet, "rename", []any{userID, textureID, name}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *ClosetClient) Remove(ctx context.Context, userID, textureID int64, refund bool) (int64, error) {
	if c.host == nil {
		return 0, errors.New("host service unavailable")
	}
	var out struct {
		Balance int64 `json:"balance"`
	}
	if err := callHostOp(ctx, c.host.Closet, "remove", []any{userID, textureID, refund}, &out); err != nil {
		return 0, err
	}
	return out.Balance, nil
}

type ScoreClient struct{ host pluginv1.HostServiceClient }

func (c *ScoreClient) Balance(ctx context.Context, userID int64) (int64, error) {
	if c.host == nil {
		return 0, errors.New("host service unavailable")
	}
	var bal int64
	if err := callHostOp(ctx, c.host.Score, "balance", []any{userID}, &bal); err != nil {
		return 0, err
	}
	return bal, nil
}

func (c *ScoreClient) Apply(ctx context.Context, userID, delta int64, reason, refType, refID string) (int64, error) {
	if c.host == nil {
		return 0, errors.New("host service unavailable")
	}
	var raw json.RawMessage
	if err := callHostOp(ctx, c.host.Score, "apply", []any{userID, delta, reason, refType, refID}, &raw); err != nil {
		return 0, err
	}
	var bal int64
	if err := json.Unmarshal(raw, &bal); err == nil {
		return bal, nil
	}
	var obj struct {
		Balance int64 `json:"balance"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return 0, err
	}
	return obj.Balance, nil
}

type NotifyClient struct{ host pluginv1.HostServiceClient }

func (c *NotifyClient) Send(ctx context.Context, userIDs []int64, title, markdown string) (int, error) {
	if c.host == nil {
		return 0, errors.New("host service unavailable")
	}
	var out struct {
		Sent int `json:"sent"`
	}
	if err := callHostOp(ctx, c.host.Notify, "send", []any{userIDs, title, markdown}, &out); err != nil {
		return 0, err
	}
	return out.Sent, nil
}

func (c *NotifyClient) Announce(ctx context.Context, audience, title, markdown string) (int64, error) {
	if c.host == nil {
		return 0, errors.New("host service unavailable")
	}
	var out struct {
		ID int64 `json:"id"`
	}
	if err := callHostOp(ctx, c.host.Notify, "announce", []any{audience, title, markdown}, &out); err != nil {
		return 0, err
	}
	return out.ID, nil
}

type MailClient struct{ host pluginv1.HostServiceClient }

func (c *MailClient) Send(ctx context.Context, opts MailOptions) error {
	if c.host == nil {
		return errors.New("host service unavailable")
	}
	raw, err := json.Marshal(opts)
	if err != nil {
		return err
	}
	_, err = c.host.Mail(ctx, &pluginv1.MailSendRequest{
		OptionsJson: raw,
		Locale:      opts.Locale,
	})
	return err
}

type KVClient struct{ host pluginv1.HostServiceClient }

func (c *KVClient) Get(ctx context.Context, key string, dst any) (bool, error) {
	raw, found, err := c.GetRaw(ctx, key)
	if err != nil || !found {
		return found, err
	}
	if dst == nil {
		return true, nil
	}
	return true, json.Unmarshal(raw, dst)
}

func (c *KVClient) GetRaw(ctx context.Context, key string) (json.RawMessage, bool, error) {
	if c.host == nil {
		return nil, false, errors.New("host service unavailable")
	}
	rawArgs, _ := json.Marshal([]any{key})
	resp, err := c.host.KV(ctx, &pluginv1.HostOpRequest{Op: "get", ArgsJson: rawArgs})
	if err != nil {
		return nil, false, err
	}
	if len(resp.GetResultJson()) == 0 || string(resp.GetResultJson()) == "null" {
		return nil, false, nil
	}
	return json.RawMessage(resp.GetResultJson()), true, nil
}

func (c *KVClient) Set(ctx context.Context, key string, val any, ttlSeconds float64) error {
	if c.host == nil {
		return errors.New("host service unavailable")
	}
	return callHostOp(ctx, c.host.KV, "set", []any{key, val, ttlSeconds}, nil)
}

func (c *KVClient) Delete(ctx context.Context, key string) (bool, error) {
	if c.host == nil {
		return false, errors.New("host service unavailable")
	}
	var deleted bool
	if err := callHostOp(ctx, c.host.KV, "delete", []any{key}, &deleted); err != nil {
		return false, err
	}
	return deleted, nil
}

func (c *KVClient) List(ctx context.Context, prefix string, limit int) ([]KVEntry, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	var out []KVEntry
	if err := callHostOp(ctx, c.host.KV, "list", []any{prefix, limit}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *KVClient) Incr(ctx context.Context, key string, delta float64) (float64, error) {
	if c.host == nil {
		return 0, errors.New("host service unavailable")
	}
	var next float64
	if err := callHostOp(ctx, c.host.KV, "incr", []any{key, delta}, &next); err != nil {
		return 0, err
	}
	return next, nil
}

type DocsClient struct{ host pluginv1.HostServiceClient }

func (c *DocsClient) InsertOne(ctx context.Context, collection string, doc map[string]any) (map[string]any, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	var out map[string]any
	if err := callHostOp(ctx, c.host.Docs, "insertOne", []any{collection, doc}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *DocsClient) FindOne(ctx context.Context, collection string, filter map[string]any) (map[string]any, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	var out map[string]any
	if err := callHostOp(ctx, c.host.Docs, "findOne", []any{collection, filter}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *DocsClient) Find(ctx context.Context, collection string, filter map[string]any, opts FindOpts) (*DocsFindResult, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	var out DocsFindResult
	if err := callHostOp(ctx, c.host.Docs, "find", []any{collection, filter, opts}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func decodeBoolOrCount(raw json.RawMessage) (int64, error) {
	if len(raw) == 0 {
		return 0, nil
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		if b {
			return 1, nil
		}
		return 0, nil
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, err
	}
	return n, nil
}

func (c *DocsClient) UpdateOne(ctx context.Context, collection string, filter, patch map[string]any) (int64, error) {
	if c.host == nil {
		return 0, errors.New("host service unavailable")
	}
	var out struct {
		Modified json.RawMessage `json:"modified"`
	}
	if err := callHostOp(ctx, c.host.Docs, "updateOne", []any{collection, filter, patch}, &out); err != nil {
		return 0, err
	}
	return decodeBoolOrCount(out.Modified)
}

func (c *DocsClient) UpdateMany(ctx context.Context, collection string, filter, patch map[string]any) (int64, error) {
	if c.host == nil {
		return 0, errors.New("host service unavailable")
	}
	var out struct {
		Modified json.RawMessage `json:"modified"`
	}
	if err := callHostOp(ctx, c.host.Docs, "updateMany", []any{collection, filter, patch}, &out); err != nil {
		return 0, err
	}
	return decodeBoolOrCount(out.Modified)
}

func (c *DocsClient) DeleteOne(ctx context.Context, collection string, filter map[string]any) (int64, error) {
	if c.host == nil {
		return 0, errors.New("host service unavailable")
	}
	var out struct {
		Deleted json.RawMessage `json:"deleted"`
	}
	if err := callHostOp(ctx, c.host.Docs, "deleteOne", []any{collection, filter}, &out); err != nil {
		return 0, err
	}
	return decodeBoolOrCount(out.Deleted)
}

func (c *DocsClient) Count(ctx context.Context, collection string, filter map[string]any) (int64, error) {
	if c.host == nil {
		return 0, errors.New("host service unavailable")
	}
	var n int64
	if err := callHostOp(ctx, c.host.Docs, "count", []any{collection, filter}, &n); err != nil {
		return 0, err
	}
	return n, nil
}

type FilesClient struct{ host pluginv1.HostServiceClient }

func (c *FilesClient) Put(ctx context.Context, key string, data []byte) (string, error) {
	if c.host == nil {
		return "", errors.New("host service unavailable")
	}
	resp, err := c.host.FilesPut(ctx, &pluginv1.FilesPutRequest{Key: key, Data: data})
	if err != nil {
		return "", err
	}
	return resp.GetUrl(), nil
}

func (c *FilesClient) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if c.host == nil {
		return nil, false, errors.New("host service unavailable")
	}
	resp, err := c.host.FilesGet(ctx, &pluginv1.FilesKeyRequest{Key: key})
	if err != nil {
		return nil, false, err
	}
	return resp.GetData(), resp.GetFound(), nil
}

func (c *FilesClient) Delete(ctx context.Context, key string) (bool, error) {
	if c.host == nil {
		return false, errors.New("host service unavailable")
	}
	resp, err := c.host.FilesDelete(ctx, &pluginv1.FilesKeyRequest{Key: key})
	if err != nil {
		return false, err
	}
	return resp.GetDeleted(), nil
}

func (c *FilesClient) URL(ctx context.Context, key string) (string, error) {
	if c.host == nil {
		return "", errors.New("host service unavailable")
	}
	resp, err := c.host.FilesURL(ctx, &pluginv1.FilesKeyRequest{Key: key})
	if err != nil {
		return "", err
	}
	return resp.GetUrl(), nil
}

type FetchClient struct{ host pluginv1.HostServiceClient }

func (c *FetchClient) Do(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	resp, err := c.host.Fetch(ctx, &pluginv1.FetchRequest{
		Url:       req.URL,
		Method:    req.Method,
		Headers:   req.Headers,
		Body:      req.Body,
		TimeoutMs: int32(req.TimeoutMs),
	})
	if err != nil {
		return nil, err
	}
	return &FetchResponse{
		OK:         resp.GetOk(),
		Status:     int(resp.GetStatus()),
		StatusText: resp.GetStatusText(),
		Headers:    resp.GetHeaders(),
		URL:        resp.GetUrl(),
		Body:       resp.GetBody(),
	}, nil
}

type SiteClient struct{ host pluginv1.HostServiceClient }

func (c *SiteClient) Name(ctx context.Context, locale string) (string, error) {
	if c.host == nil {
		return "", errors.New("host service unavailable")
	}
	var out string
	if err := callHostOpLocale(ctx, c.host.Site, "name", []any{locale}, locale, nil, &out); err != nil {
		return "", err
	}
	return out, nil
}

func (c *SiteClient) URL(ctx context.Context) (string, error) {
	if c.host == nil {
		return "", errors.New("host service unavailable")
	}
	var out string
	if err := callHostOp(ctx, c.host.Site, "url", nil, &out); err != nil {
		return "", err
	}
	return out, nil
}

func (c *SiteClient) Description(ctx context.Context, locale string) (string, error) {
	if c.host == nil {
		return "", errors.New("host service unavailable")
	}
	var out string
	if err := callHostOpLocale(ctx, c.host.Site, "description", []any{locale}, locale, nil, &out); err != nil {
		return "", err
	}
	return out, nil
}

func (c *SiteClient) Locales(ctx context.Context) ([]string, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	var out []string
	if err := callHostOp(ctx, c.host.Site, "locales", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *SiteClient) Public(ctx context.Context) (map[string]any, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	var out map[string]any
	if err := callHostOp(ctx, c.host.Site, "public", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

type AuditClient struct{ host pluginv1.HostServiceClient }

func (c *AuditClient) Record(ctx context.Context, action, targetType, targetID string, meta map[string]any) error {
	if c.host == nil {
		return errors.New("host service unavailable")
	}
	var raw []byte
	if meta != nil {
		var err error
		raw, err = json.Marshal(meta)
		if err != nil {
			return err
		}
	}
	_, err := c.host.Audit(ctx, &pluginv1.AuditRecordRequest{
		Action:     action,
		TargetType: targetType,
		TargetId:   targetID,
		MetaJson:   raw,
	})
	return err
}

type CacheClient struct{ host pluginv1.HostServiceClient }

func (c *CacheClient) Get(ctx context.Context, key string, dst any) (bool, error) {
	if c.host == nil {
		return false, errors.New("host service unavailable")
	}
	rawArgs, _ := json.Marshal([]any{key})
	resp, err := c.host.Cache(ctx, &pluginv1.HostOpRequest{Op: "get", ArgsJson: rawArgs})
	if err != nil {
		return false, err
	}
	if len(resp.GetResultJson()) == 0 || string(resp.GetResultJson()) == "null" {
		return false, nil
	}
	if dst == nil {
		return true, nil
	}
	return true, json.Unmarshal(resp.GetResultJson(), dst)
}

func (c *CacheClient) Set(ctx context.Context, key string, val any, ttlSeconds float64) error {
	if c.host == nil {
		return errors.New("host service unavailable")
	}
	return callHostOp(ctx, c.host.Cache, "set", []any{key, val, ttlSeconds}, nil)
}

func (c *CacheClient) Delete(ctx context.Context, key string) (bool, error) {
	if c.host == nil {
		return false, errors.New("host service unavailable")
	}
	var deleted bool
	if err := callHostOp(ctx, c.host.Cache, "delete", []any{key}, &deleted); err != nil {
		return false, err
	}
	return deleted, nil
}

type I18nClient struct{ host pluginv1.HostServiceClient }

func (c *I18nClient) T(ctx context.Context, key string, params map[string]any, locale string) (string, error) {
	if c.host == nil {
		return key, errors.New("host service unavailable")
	}
	var out string
	if err := callHostOpLocale(ctx, c.host.I18N, "t", []any{key, params, locale}, locale, nil, &out); err != nil {
		return key, err
	}
	return out, nil
}

func (c *I18nClient) Locales(ctx context.Context) ([]string, error) {
	if c.host == nil {
		return nil, errors.New("host service unavailable")
	}
	var out []string
	if err := callHostOp(ctx, c.host.I18N, "locales", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

type LogClient struct{ host pluginv1.HostServiceClient }

func (c *LogClient) emit(level, msg string) {
	if c.host == nil || msg == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = c.host.Log(ctx, &pluginv1.LogRequest{Level: level, Message: msg})
}

func (c *LogClient) Debug(msg string) { c.emit("debug", msg) }
func (c *LogClient) Info(msg string)  { c.emit("info", msg) }
func (c *LogClient) Warn(msg string)  { c.emit("warn", msg) }
func (c *LogClient) Error(msg string) { c.emit("error", msg) }
