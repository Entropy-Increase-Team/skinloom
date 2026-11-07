package account

import (
	"context"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	entplayer "github.com/Entropy-Increase-Team/skinloom/ent/player"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
)

// announcePlayers publishes ev in tx for every player of the account. A status change
// (banned, deleted, restored) changes what game clients get for all of them, and
// removing the account removes them: listeners such as the CDN purge must hear about
// each player, by name (ADR 0039). Nothing is read when nobody listens.
func (s *Service) announcePlayers(ctx context.Context, tx *ent.Client, userID int64, ev hook.Event[hook.PlayerEvent]) error {
	if !ev.Listening(s.hooks) {
		return nil
	}
	players, err := tx.Player.Query().Where(entplayer.UserID(userID)).
		Select(entplayer.FieldID, entplayer.FieldUserID, entplayer.FieldName, entplayer.FieldUUID).All(ctx)
	if err != nil {
		return err
	}
	for _, p := range players {
		if err := ev.Publish(ctx, s.hooks, tx, hook.PlayerEvent{PlayerID: p.ID, UserID: p.UserID, Name: p.Name, UUID: p.UUID}); err != nil {
			return err
		}
	}
	return nil
}
