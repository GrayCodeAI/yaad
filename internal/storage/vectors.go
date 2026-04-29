package storage

import (
	"encoding/binary"
	"math"
)

// EncodeVector encodes a float32 slice to a byte slice for SQLite BLOB storage.
func EncodeVector(v []float32) []byte {
	b := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

// DecodeVector decodes a byte slice from SQLite BLOB to a float32 slice.
func DecodeVector(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

// SaveEmbedding stores a vector embedding for a node.
func (s *Store) SaveEmbedding(nodeID, model string, vector []float32) error {
	_, err := s.db.Exec(
		`INSERT INTO embeddings(node_id, vector, model) VALUES(?,?,?)
		 ON CONFLICT(node_id) DO UPDATE SET vector=excluded.vector, model=excluded.model`,
		nodeID, EncodeVector(vector), model)
	return err
}

// GetEmbedding retrieves the embedding for a node.
func (s *Store) GetEmbedding(nodeID string) ([]float32, string, error) {
	var blob []byte
	var model string
	err := s.db.QueryRow(`SELECT vector, model FROM embeddings WHERE node_id=?`, nodeID).Scan(&blob, &model)
	if err != nil {
		return nil, "", err
	}
	return DecodeVector(blob), model, nil
}

// DeleteEmbedding removes a vector embedding for a node.
func (s *Store) DeleteEmbedding(nodeID string) error {
	_, err := s.db.Exec(`DELETE FROM embeddings WHERE node_id=?`, nodeID)
	return err
}

// GetEmbeddingsBatch returns a paginated batch of embeddings.
func (s *Store) GetEmbeddingsBatch(offset, limit int) (map[string][]float32, error) {
	rows, err := s.db.Query(`SELECT node_id, vector FROM embeddings LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string][]float32{}
	for rows.Next() {
		var nodeID string
		var blob []byte
		if err := rows.Scan(&nodeID, &blob); err != nil {
			return nil, err
		}
		result[nodeID] = DecodeVector(blob)
	}
	return result, rows.Err()
}

// AllEmbeddings returns all stored embeddings as (nodeID, vector) pairs.
func (s *Store) AllEmbeddings() (map[string][]float32, error) {
	rows, err := s.db.Query(`SELECT node_id, vector FROM embeddings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string][]float32{}
	for rows.Next() {
		var nodeID string
		var blob []byte
		if err := rows.Scan(&nodeID, &blob); err != nil {
			return nil, err
		}
		result[nodeID] = DecodeVector(blob)
	}
	return result, rows.Err()
}
