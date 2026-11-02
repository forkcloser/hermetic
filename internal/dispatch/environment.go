package dispatch

import "strings"

// aquaPrefix is the library's variables', none of which reaches a tool.
const aquaPrefix = "AQUA_"

// Environment is the environment a dispatched tool runs in: the shell's,
// minus every AQUA_* variable. Nothing of ours is added: the toolset is reached
// through hermetic, and a stock aqua under the tool is the human's own.
func Environment(environ []string) []string {
	out := make([]string, 0, len(environ))

	for _, entry := range environ {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, aquaPrefix) {
			continue
		}

		out = append(out, entry)
	}

	return out
}
