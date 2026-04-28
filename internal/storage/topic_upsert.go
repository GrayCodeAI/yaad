package storage

// UpsertByTopic updates an existing node if one with the same project+scope+topic_key exists,
// otherwise creates a new one. Based on Engram's topic dedup approach.
// topic_key is stored in the Tags field as "topic:<key>".
func (s *Store) UpsertByTopic(n *Node, topicKey string) (*Node, bool, error) {
	if topicKey == "" {
		return nil, false, s.CreateNode(n)
	}

	tag := "topic:" + topicKey

	// Find existing node with same topic key in same project+scope
	existing, err := s.ListNodes(NodeFilter{
		Project: n.Project,
		Scope:   n.Scope,
		Type:    n.Type,
	})
	if err != nil {
		return nil, false, err
	}

	for _, e := range existing {
		if containsTag(e.Tags, tag) {
			// Update existing node
			s.SaveVersion(e.ID, e.Content, "topic-upsert", "updated via topic key: "+topicKey)
			e.Content = n.Content
			e.Summary = n.Summary
			e.Version++
			e.Confidence = n.Confidence
			if err := s.UpdateNode(e); err != nil {
				return nil, false, err
			}
			return e, true, nil // updated
		}
	}

	// No existing node — create new with topic tag
	if n.Tags == "" {
		n.Tags = tag
	} else {
		n.Tags = n.Tags + "," + tag
	}
	return n, false, s.CreateNode(n) // created
}

func containsTag(tags, tag string) bool {
	if tags == "" {
		return false
	}
	for _, t := range splitCSV(tags) {
		if t == tag {
			return true
		}
	}
	return false
}

func splitCSV(s string) []string {
	var result []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if i > start {
				result = append(result, s[start:i])
			}
			start = i + 1
		}
	}
	return result
}
