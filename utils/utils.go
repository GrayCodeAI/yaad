package utils

// ShortID safely truncates an ID to 8 chars for display.
func ShortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
