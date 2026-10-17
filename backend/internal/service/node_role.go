package service

import (
	"os"
	"strings"
)

// A secondary serves gateway traffic and keeps request-local workers running,
// but does not run primary-owned paid probes or account maintenance schedules.
func primarySchedulesEnabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv("SUB2API_NODE_ROLE")), "secondary")
}

// Startup prefix cleanup assumes every other process is dead. In a shared Redis
// deployment that would remove live requests on the other node. Normal TTL-based
// slot cleanup remains enabled on every node.
func exclusiveStartupSlotCleanupEnabled() bool {
	return os.Getenv("SUB2API_MULTI_NODE") != "1"
}
