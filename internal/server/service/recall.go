package service

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Recall returns any object by ID with the IDs of related objects. The
// summary is short on purpose: the caller follows links for more.
func Recall(ctx context.Context, a model.Actor, id model.ID) (protocol.Object, error) {
	var obj protocol.Object
	err := model.Snapshot(ctx, func(tx *sql.Tx) error {
		var err error
		obj, err = recall(ctx, tx, a, id)
		return err
	})
	return obj, err
}

func recall(ctx context.Context, db model.Querier, a model.Actor, id model.ID) (protocol.Object, error) {
	obj := protocol.Object{ID: string(id), Links: map[string]string{}}
	switch id.Prefix() {
	case model.PrefixMessage:
		m, err := model.GetMessage(ctx, db, id)
		if err != nil {
			return obj, err
		}
		if err := owned(a, m.OwnerID); err != nil {
			return obj, err
		}
		if err := requireManager(a); err != nil {
			return obj, errs.ErrNotFound
		}
		obj.Type = "message"
		obj.Summary = fmt.Sprintf("%s by %s:%s: %s", m.Kind, m.Author.Kind, m.Author.ID, clip(m.Text, 600))
		if m.Author.ID != "" {
			obj.Links["author"] = string(m.Author.ID)
		}
	case model.PrefixAgent:
		ag, err := model.GetAgent(ctx, db, id)
		if err != nil {
			return obj, err
		}
		m, err := model.GetMachine(ctx, db, ag.MachineID)
		if err != nil {
			return obj, err
		}
		if err := owned(a, m.OwnerID); err != nil {
			return obj, err
		}
		obj.Type = "agent"
		obj.Summary = fmt.Sprintf("Agent %s@%s (%s, %s).", ag.Name, m.Name, ag.Runtime, onlineWord(m.Online))
		obj.Links["machine"] = string(m.ID)
	case model.PrefixMachine:
		m, err := model.GetMachine(ctx, db, id)
		if err != nil {
			return obj, err
		}
		if err := owned(a, m.OwnerID); err != nil {
			return obj, err
		}
		obj.Type = "machine"
		obj.Summary = fmt.Sprintf("Machine %q (%s, %s, %d free slots).", m.Name, m.State, onlineWord(m.Online), m.Slots)
	default:
		return obj, fmt.Errorf("%w: cannot recall %q", errs.ErrNotFound, id)
	}
	if len(obj.Links) == 0 {
		obj.Links = nil
	}
	return obj, nil
}

func onlineWord(on bool) string {
	if on {
		return "online"
	}
	return "offline"
}
