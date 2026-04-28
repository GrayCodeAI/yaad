package storage

import "time"

// ReplayEvent is a raw tool event stored for session replay.
type ReplayEvent struct {
	ID        int64
	SessionID string
	Data      string // JSON
	CreatedAt time.Time
}

// AddReplayEvent stores a tool event for replay.
func (s *Store) AddReplayEvent(sessionID, data string) error {
	_, err := s.db.Exec(
		`INSERT INTO replay_events(session_id, data, created_at) VALUES(?,?,?)`,
		sessionID, data, time.Now())
	return err
}

// GetReplayEvents returns all events for a session in order.
func (s *Store) GetReplayEvents(sessionID string) ([]*ReplayEvent, error) {
	rows, err := s.db.Query(
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
