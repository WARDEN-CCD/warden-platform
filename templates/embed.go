// Package templates embeds the golden-path service template rendered by `warden new`.
package templates

import "embed"

// FS holds templates/spring-service, including dotfiles (.github, .mvn, .gitignore).
//
//go:embed all:spring-service
var FS embed.FS
