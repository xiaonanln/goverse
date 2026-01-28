package cluster

import "time"

// DefaultCallTimeout is the default timeout for CallObject operations when no context deadline is provided.
// This prevents indefinite hangs when clients use context.Background() without a deadline.
// Set to 30 seconds to allow for method execution while still providing protection against runaway operations.
const DefaultCallTimeout = 30 * time.Second
