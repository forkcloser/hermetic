package hermetic

import (
	"net/http"
	"time"

	"github.com/mycophonic/primordium/network"
	"github.com/mycophonic/primordium/network/transporter"
)

// The library's own GitHub client wraps the client below in a retrying one of
// its own (retryablehttp, four retries), so this policy stays short: the two
// multiply on a GitHub API call.
const (
	maxRetries     = 3
	initialBackoff = time.Second
	maxBackoff     = 30 * time.Second
)

// useTransporter makes the process's HTTP the toolset's: TLS 1.3 with
// post-quantum hybrid key exchange, and primordium's retry and backoff. The
// library takes no client: every command it runs, its downloads and its
// GitHub API calls alike, is handed http.DefaultClient (or builds on its
// transport), so replacing that client is the one place to set it. Once per
// process, before any request: SetDefaults panics on a second call, and a
// client made earlier keeps Go's default TLS.
func useTransporter() {
	network.SetDefaults()

	//nolint:reassign // the library is handed http.DefaultClient and takes no other.
	http.DefaultClient = transporter.NewClient(transporter.Options{
		MaxRetries:     maxRetries,
		InitialBackoff: initialBackoff,
		MaxBackoff:     maxBackoff,
		UserAgent:      Name,
	})
}
