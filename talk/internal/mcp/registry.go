package mcp

import (
	"fmt"
	"regexp"

	"github.com/pixime-net/talk/internal/domain"
)

// ToolNameSeparator joins a server name and a remote tool name into the
// namespaced tool name exposed to the LLM (e.g. "owm__geocode").
const ToolNameSeparator = "__"

// MaxServerNameLength bounds server names so namespaced tool names stay within
// the 64-character limit imposed by LLM providers on function names.
const MaxServerNameLength = 24

// serverNameRe keeps server names usable verbatim as a tool name prefix:
// lowercase alphanumerics and hyphens only. Underscores are excluded so that
// ToolNameSeparator can never appear inside the prefix itself.
var serverNameRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,22}[a-z0-9])?$`)

// ValidateServerName reports whether name can be used as a tool namespace prefix.
func ValidateServerName(name string) error {
	if !serverNameRe.MatchString(name) {
		return fmt.Errorf(
			"must be 1 to %d characters, lowercase letters, digits or hyphens, starting and ending with a letter or digit (no underscore)",
			MaxServerNameLength,
		)
	}
	return nil
}

type AuthType = domain.MCPAuthType
type OAuthConfig = domain.MCPOAuthConfig
type ServerConfig = domain.MCPServerConfig
type Registry = domain.MCPRegistry

const (
	AuthTypeNone   = domain.MCPAuthTypeNone
	AuthTypeAPIKey = domain.MCPAuthTypeAPIKey
	AuthTypeOAuth  = domain.MCPAuthTypeOAuth
)
