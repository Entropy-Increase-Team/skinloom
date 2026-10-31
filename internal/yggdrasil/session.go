package yggdrasil

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	entplayer "github.com/Entropy-Increase-Team/skinloom/ent/player"
	tex "github.com/Entropy-Increase-Team/skinloom/ent/texture"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/ent/yggsession"
	"github.com/Entropy-Increase-Team/skinloom/ent/yggtoken"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/validate"
	"github.com/Entropy-Increase-Team/skinloom/internal/player"
	"github.com/Entropy-Increase-Team/skinloom/internal/score"
	"github.com/Entropy-Increase-Team/skinloom/internal/texture"
)

// JoinWindow is how long a join waits for the game server's hasJoined.
const JoinWindow = 30 * time.Second

// Property is a profile property, signed when asked for.
type Property struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	Signature string `json:"signature,omitempty"`
}

// Profile is a profile as sessionserver returns it.
type Profile struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Properties []Property `json:"properties"`
}

type textureRef struct {
	URL      string            `json:"url"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// texturesPayload is the decoded "textures" property. The timestamp is when the player
// last changed, not now, so the value (and its signature) stays the same between
// changes and can be cached.
type texturesPayload struct {
	Timestamp         int64                 `json:"timestamp"`
	ProfileID         string                `json:"profileId"`
	ProfileName       string                `json:"profileName"`
	SignatureRequired bool                  `json:"signatureRequired,omitempty"`
	Textures          map[string]textureRef `json:"textures"`
}

// activePlayer loads a player of an active account (with what it wears), or nil.
func (s *Service) activePlayer(ctx context.Context, where ...func(*ent.PlayerQuery)) (*ent.Player, error) {
	q := s.db.Player.Query().
		WithOwner(func(q *ent.UserQuery) { q.Select(user.FieldID, user.FieldStatus) }).
		WithSkin(func(q *ent.TextureQuery) { q.Select(tex.FieldID, tex.FieldHash, tex.FieldType) }).
		WithCape(func(q *ent.TextureQuery) { q.Select(tex.FieldID, tex.FieldHash) })
	for _, w := range where {
		w(q)
	}
	p, err := q.Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if p.Edges.Owner.Status != user.StatusActive {
		return nil, nil // banned and deleted owners' players do not exist for game servers
	}
	return p, nil
}

func byUUID(id string) func(*ent.PlayerQuery) {
	return func(q *ent.PlayerQuery) { q.Where(entplayer.UUID(id)) }
}

// profile serializes a player. base is the site URL textures are under.
func (s *Service) profile(ctx context.Context, p *ent.Player, base string, signed bool) (*Profile, error) {
	payload := texturesPayload{Timestamp: p.UpdatedAt.UnixMilli(), ProfileID: p.UUID, ProfileName: p.Name,
		SignatureRequired: signed, Textures: map[string]textureRef{}}
	if t := p.Edges.Skin; t != nil {
		ref := textureRef{URL: base + "/textures/" + t.Hash}
		if t.Type == tex.TypeAlex {
			ref.Metadata = map[string]string{"model": "slim"}
		}
		payload.Textures["SKIN"] = ref
	}
	if t := p.Edges.Cape; t != nil {
		payload.Textures["CAPE"] = textureRef{URL: base + "/textures/" + t.Hash}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	out := &Profile{ID: p.UUID, Name: p.Name, Properties: []Property{
		{Name: "textures", Value: base64.StdEncoding.EncodeToString(raw)},
		{Name: "uploadableTextures", Value: "skin,cape"},
	}}
	if signed {
		for i := range out.Properties {
			if out.Properties[i].Signature, err = s.sign(ctx, out.Properties[i].Value); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// sign signs a property value with SHA1withRSA, as Mojang and authlib-injector do.
// Signatures are cached: a 4096-bit signature costs milliseconds, and the values only
// change when a player does.
func (s *Service) sign(ctx context.Context, value string) (string, error) {
	s.sigMu.Lock()
	sig, ok := s.sigs[value]
	s.sigMu.Unlock()
	if ok {
		return sig, nil
	}
	k, err := s.keys(ctx)
	if err != nil {
		return "", err
	}
	sum := sha1.Sum([]byte(value))
	raw, err := rsa.SignPKCS1v15(rand.Reader, k.signing, crypto.SHA1, sum[:])
	if err != nil {
		return "", err
	}
	sig = base64.StdEncoding.EncodeToString(raw)
	s.sigMu.Lock()
	if len(s.sigs) >= 10000 {
		s.sigs = map[string]string{} // bounded; refilled by the next requests
	}
	s.sigs[value] = sig
	s.sigMu.Unlock()
	return sig, nil
}

// ProfileByUUID is sessionserver's profile/{uuid}; nil when there is no such player.
func (s *Service) ProfileByUUID(ctx context.Context, id, base string, signed bool) (*Profile, error) {
	id, ok := normalizeUUID(id)
	if !ok {
		return nil, nil
	}
	p, err := s.activePlayer(ctx, byUUID(id))
	if err != nil || p == nil {
		return nil, err
	}
	return s.profile(ctx, p, base, signed)
}

// Join records that the token's player is joining a game server.
func (s *Service) Join(ctx context.Context, token, selectedProfile, serverID, ip string) error {
	if serverID == "" || len(serverID) > 128 {
		return illegal("Invalid serverId.")
	}
	t, err := s.find(ctx, token, "", false)
	if err != nil {
		return err
	}
	id, _ := normalizeUUID(selectedProfile)
	if t.player == nil || t.player.UUID != id {
		return errInvalidToken
	}
	return s.db.YggSession.Create().SetServerID(serverID).SetPlayerID(t.player.ID).SetIP(ip).
		SetExpiresAt(s.now().Add(JoinWindow)).
		OnConflictColumns(yggsession.FieldServerID).UpdateNewValues().Exec(ctx)
}

// HasJoined is the game server's check of a join: the signed profile, or nil.
// Blessing Skin answered 500 when the player was deleted in between.
func (s *Service) HasJoined(ctx context.Context, username, serverID, ip, base string) (*Profile, error) {
	sess, err := s.db.YggSession.Query().Where(yggsession.ServerID(serverID), yggsession.ExpiresAtGT(s.now())).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if ip != "" && ip != sess.IP {
		return nil, nil
	}
	p, err := s.activePlayer(ctx, func(q *ent.PlayerQuery) { q.Where(entplayer.ID(sess.PlayerID)) })
	if err != nil || p == nil || !strings.EqualFold(p.Name, username) {
		return nil, err
	}
	return s.profile(ctx, p, base, true)
}

// LookupNames is the bulk name → profile lookup, in one query (Blessing Skin made one
// per name); names that are not players are left out.
func (s *Service) LookupNames(ctx context.Context, names []string, p Policy) ([]ProfileRef, error) {
	keys := map[string]bool{}
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			keys[player.Key(n)] = true
		}
	}
	if len(keys) > p.SearchMax {
		return nil, forbidden("Too many names: at most " + strconv.Itoa(p.SearchMax) + ".")
	}
	out := []ProfileRef{}
	if len(keys) == 0 {
		return out, nil
	}
	list := make([]string, 0, len(keys))
	for k := range keys {
		list = append(list, k)
	}
	players, err := s.db.Player.Query().Where(entplayer.NameLowerIn(list...),
		entplayer.HasOwnerWith(user.StatusEQ(user.StatusActive))).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, pl := range players {
		out = append(out, ProfileRef{ID: pl.UUID, Name: pl.Name})
	}
	return out, nil
}

// LookupName is the single name → profile lookup; nil when there is no such player.
func (s *Service) LookupName(ctx context.Context, name string) (*ProfileRef, error) {
	p, err := s.activePlayer(ctx, func(q *ent.PlayerQuery) { q.Where(entplayer.NameLower(player.Key(name))) })
	if err != nil || p == nil {
		return nil, err
	}
	return &ProfileRef{ID: p.UUID, Name: p.Name}, nil
}

// owned checks a Bearer token for the texture API: valid, and the player is its
// user's (Blessing Skin accepted any valid token for any player), and the account may
// change textures: one that has to verify its email first may not.
func (s *Service) owned(ctx context.Context, token, profileID string, pol Policy) (*found, *ent.Player, error) {
	t, err := s.find(ctx, token, "", false)
	if errors.Is(err, errInvalidToken) {
		return nil, nil, errUnauthorized
	}
	if err != nil {
		return nil, nil, err
	}
	id, ok := normalizeUUID(profileID)
	if !ok {
		return nil, nil, &Error{http.StatusNotFound, "NotFoundException", "Profile not found."}
	}
	p, err := s.activePlayer(ctx, byUUID(id))
	if err != nil {
		return nil, nil, err
	}
	if p == nil {
		return nil, nil, &Error{http.StatusNotFound, "NotFoundException", "Profile not found."}
	}
	if p.UserID != t.user.ID {
		return nil, nil, forbidden("Profile not owned by this token.")
	}
	if pol.RequireVerified && t.user.EmailVerifiedAt == nil {
		return nil, nil, errUnverified
	}
	return t, p, nil
}

// Upload is a texture uploaded through the Yggdrasil API.
type Upload struct {
	Kind string // skin | cape
	Slim bool   // model=slim
	Name string // the file name, without .png
	Data []byte
}

// UploadTexture uploads a texture for the token's own player and puts it on: a
// private texture (as Blessing Skin made them), charged like any upload, or the same
// image already in the library, added to the closet.
func (s *Service) UploadTexture(ctx context.Context, token, profileID string, in Upload, pol Policy, tp texture.Policy, r score.Rates) error {
	t, p, err := s.owned(ctx, token, profileID, pol)
	if err != nil {
		return err
	}
	typ := "cape"
	if in.Kind == "skin" {
		typ = "steve"
		if in.Slim {
			typ = "alex"
		}
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = p.Name + " " + in.Kind
	}
	res, err := s.textures.Upload(ctx, t.user.ID, texture.UploadInput{Name: name, Type: typ, Public: false, Data: in.Data}, tp, r)
	if err != nil {
		return uploadError(err)
	}
	viewer := texture.Viewer{UserID: t.user.ID}
	if has, err := s.textures.InCloset(ctx, t.user.ID, res.Texture.ID); err != nil {
		return err
	} else if !has {
		if _, _, err := s.textures.Favorite(ctx, viewer, res.Texture.ID, "", r); err != nil && !errors.Is(err, texture.ErrInCloset) {
			return uploadError(err)
		}
	}
	id := res.Texture.ID
	wear := []*int64{nil, nil}
	if in.Kind == "skin" {
		wear[0] = &id
	} else {
		wear[1] = &id
	}
	if _, err := s.players.Wear(ctx, t.user.ID, p.ID, wear[0], wear[1], nil); err != nil {
		return uploadError(err)
	}
	return nil
}

// ResetTexture takes the token's own player's skin or cape off.
func (s *Service) ResetTexture(ctx context.Context, token, profileID, kind string, pol Policy) error {
	t, p, err := s.owned(ctx, token, profileID, pol)
	if err != nil {
		return err
	}
	zero := int64(0)
	wear := []*int64{nil, nil}
	if kind == "skin" {
		wear[0] = &zero
	} else {
		wear[1] = &zero
	}
	_, err = s.players.Wear(ctx, t.user.ID, p.ID, wear[0], wear[1], nil)
	return err
}

func uploadError(err error) error {
	if fields := validate.FieldErrors(err); len(fields) > 0 {
		msgs := make([]string, len(fields))
		for i, f := range fields {
			msgs[i] = f.Field + ": " + f.Message
		}
		return illegal(strings.Join(msgs, "; "))
	}
	if errors.Is(err, score.ErrInsufficient) {
		return forbidden("Not enough score.")
	}
	return err
}

// Cleanup deletes join sessions and tokens that can no longer be used.
func (s *Service) Cleanup(ctx context.Context) (int, error) {
	now := s.now()
	a, err := s.db.YggSession.Delete().Where(yggsession.ExpiresAtLTE(now)).Exec(ctx)
	if err != nil {
		return a, err
	}
	b, err := s.db.YggToken.Delete().Where(yggtoken.RefreshUntilLTE(now)).Exec(ctx)
	return a + b, err
}
