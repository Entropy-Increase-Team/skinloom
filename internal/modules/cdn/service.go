package cdn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/settings"
)

// Hook owners of the service's registrations.
const (
	owner      = "cdn"
	ownerWatch = "cdn.watch"
)

// ErrOff: no CDN is configured.
var ErrOff = errors.New("no CDN is configured")

// Service purges player URLs when player events are delivered (ADR 0028). Its
// handlers are registered only while a provider is set: events nobody listens to are
// not even written to the outbox, so a site without a CDN pays nothing.
type customPurgerEntry struct {
	owner  string
	purger Purger
}

type Service struct {
	bus      *hook.Bus
	settings *settings.Service
	client   *http.Client
	log      *slog.Logger
	mu       sync.Mutex // one sync at a time

	extMu  sync.RWMutex
	custom map[string]customPurgerEntry
}

// RegisterPurger registers a plugin CDN purger owned by owner and re-syncs hook listeners.
func (s *Service) RegisterPurger(owner, id string, p Purger) error {
	id = strings.TrimSpace(id)
	if id == "" || p == nil {
		return fmt.Errorf("invalid CDN purger %q", id)
	}
	switch id {
	case None, Cloudflare, Aliyun, Tencent, Webhook:
		return fmt.Errorf("cannot override built-in CDN provider %q", id)
	}
	s.extMu.Lock()
	if s.custom == nil {
		s.custom = map[string]customPurgerEntry{}
	}
	if existing, exists := s.custom[id]; exists && existing.owner != owner {
		s.extMu.Unlock()
		return fmt.Errorf("CDN purger %q is already registered by %s", id, existing.owner)
	}
	s.custom[id] = customPurgerEntry{owner: owner, purger: p}
	s.extMu.Unlock()
	if s.settings != nil && s.bus != nil {
		s.sync()
	}
	return nil
}

// UnregisterPurgers removes plugin CDN purgers owned by owner and re-syncs hook listeners.
func (s *Service) UnregisterPurgers(owner string) {
	s.extMu.Lock()
	removed := false
	for id, ent := range s.custom {
		if ent.owner == owner {
			delete(s.custom, id)
			removed = true
		}
	}
	s.extMu.Unlock()
	if removed && s.settings != nil && s.bus != nil {
		s.sync()
	}
}

// CustomProviders returns sorted IDs of currently registered plugin CDN providers.
func (s *Service) CustomProviders() []any {
	s.extMu.RLock()
	defer s.extMu.RUnlock()
	ids := make([]string, 0, len(s.custom))
	for id := range s.custom {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

func (s *Service) resolvePurger(cfg Config) (Purger, error) {
	if cfg.Provider != None && cfg.Provider != "" {
		s.extMu.RLock()
		ent, ok := s.custom[cfg.Provider]
		s.extMu.RUnlock()
		if ok && ent.purger != nil {
			if cfg.BaseURL == "" {
				return nil, fmt.Errorf("%w: set cdn.base_url or site.url", ErrIncomplete)
			}
			return ent.purger, nil
		}
	}
	p, err := cfg.Purger(s.client)
	if err != nil && !errors.Is(err, ErrOff) && !errors.Is(err, ErrIncomplete) {
		return nil, fmt.Errorf("%w: %v", ErrIncomplete, err)
	}
	return p, err
}

// NewService builds the service; call Start once the settings are loaded.
func NewService(bus *hook.Bus, s *settings.Service, log *slog.Logger) *Service {
	client := &http.Client{
		Timeout: 15 * time.Second,
		// The purge APIs answer directly; a redirect would take the request (and a
		// webhook's signed body) somewhere the settings did not name.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return &Service{bus: bus, settings: s, client: client, log: log}
}

// Start registers the handlers the current settings call for and follows changes.
func (s *Service) Start() error {
	watch := hook.OnSettingsChanged.Handle(0, func(context.Context, hook.SettingsChanged) error {
		s.sync()
		return nil
	})
	if err := s.bus.Replace(ownerWatch, watch); err != nil {
		return err
	}
	s.sync()
	return nil
}

// sync registers the purge handlers while a complete CDN setup is in effect. Settings
// changes are delivered on several workers, so syncs take turns, each reading the
// settings in effect when it runs.
func (s *Service) sync() {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.resolvePurger(ConfigFrom(s.settings.Current()))
	if err != nil {
		// Listening would only fill the outbox with events that fail and are retried.
		s.log.Warn("CDN purging is off until its settings are complete", "err", err)
	}
	if p == nil {
		s.bus.Remove(owner)
		return
	}
	purge := func(event string) func(context.Context, hook.PlayerEvent) error {
		return func(ctx context.Context, e hook.PlayerEvent) error {
			names := []string{e.Name}
			if e.OldName != "" && e.OldName != e.Name {
				names = append(names, e.OldName)
			}
			err := s.Purge(withEvent(ctx, event), names)
			if errors.Is(err, ErrOff) || errors.Is(err, ErrIncomplete) {
				return nil // turned off since the event: there is nothing to purge
			}
			return err
		}
	}
	err = s.bus.Replace(owner,
		hook.OnPlayerCreated.Handle(100, purge("player.created")), // a cached "no such player" goes
		hook.OnPlayerRenamed.Handle(100, purge("player.renamed")),
		hook.OnPlayerUpdated.Handle(100, purge("player.updated")),
		hook.OnPlayerDeleted.Handle(100, purge("player.deleted")),
	)
	if err != nil {
		s.log.Error("register CDN purging", "err", err)
	}
}

// Purge drops the cached profile URLs of the named players, with the settings in
// effect now. A failure is returned so the outbox retries the event.
func (s *Service) Purge(ctx context.Context, names []string) error {
	cfg := ConfigFrom(s.settings.Current())
	p, err := s.resolvePurger(cfg)
	if err != nil {
		return err
	}
	if p == nil {
		return ErrOff
	}
	var urls []string
	for _, n := range names {
		urls = append(urls, ProfileURLs(cfg.BaseURL, n)...)
	}
	if err := safeCallCDNPurger(ctx, p, urls); err != nil {
		s.log.Warn("CDN purge failed", "provider", cfg.Provider, "players", names, "err", err)
		return err
	}
	s.log.Info("CDN purged", "provider", cfg.Provider, "players", names, "urls", len(urls))
	return nil
}

func safeCallCDNPurger(ctx context.Context, p Purger, urls []string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("CDN purger panic: %v", r)
		}
	}()
	return p.Purge(ctx, urls)
}
