package storage

import (
	"context"
	"time"
)

// ReplayEvent is a raw tool event stored for session replay.
type ReplayEvent struct {
	ID        int64
	SessionID string
	Data      string // JSON
	CreatedAt time.Time
}

// AddReplayEvent stores a tool event for replay.
func (s *Store) AddReplayEvent(ctx context.Context, sessionID, data string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO replay_events(session_id, data, created_at) VALUES(?,?,?)`,
		sessionID, data, time.Now())
	return err
}

// GetReplayEvents returns all events for a session in order.
func (s *Store) GetReplayEvents(ctx context.Context, sessionID string) ([]*ReplayEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, data, created_at FROM replay_events WHERE session_id=? ORDER BY id`,
		sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []*ReplayEvent
	for rows.Next() {
		e := &ReplayEvent{}
		if err := rows.Scan(&e.ID, &e.SessionID, &e.Data, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
