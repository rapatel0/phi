package auth

import (
	"encoding/json"
	"net/url"
	"strings"
)

func parseAuthorizationInput(pasted string) (code, state string) {
	pasted = strings.TrimSpace(pasted)
	if pasted == "" {
		return "", ""
	}
	if strings.HasPrefix(pasted, "{") {
		var body map[string]string
		if err := json.Unmarshal([]byte(pasted), &body); err == nil {
			return body["code"], body["state"]
		}
	}
	if u, err := url.Parse(pasted); err == nil && u.IsAbs() {
		q := u.Query()
		return q.Get("code"), q.Get("state")
	}
	if i := strings.Index(pasted, "#"); i >= 0 {
		return pasted[:i], pasted[i+1:]
	}
	if strings.Contains(pasted, "code=") {
		q, err := url.ParseQuery(strings.TrimPrefix(pasted, "?"))
		if err == nil {
			return q.Get("code"), q.Get("state")
		}
	}
	return pasted, ""
}
