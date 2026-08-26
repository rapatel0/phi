package ext

import "strings"

// matcher turns a tool name into a Match function. An empty name matches every
// tool, which is what hooks.Hook already means by a nil MatchFn.
//
// Several names can be given separated by commas, because an extension that
// watches file writes has to name both edit and write, and registering the
// same handler twice would report the same work twice.
func matcher(match string) func(string) bool {
	if match == "" {
		return nil
	}
	names := make(map[string]bool)
	for n := range strings.SplitSeq(match, ",") {
		if n = strings.TrimSpace(n); n != "" {
			names[n] = true
		}
	}
	if len(names) == 0 {
		return nil
	}
	return func(tool string) bool { return names[tool] }
}
