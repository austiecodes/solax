package main

import (
	"embed"

	"github.com/egoist/mygo/ui"
)

// The icon files and their licenses are in icons/README.md.
//
//go:embed icons/*.svg
var iconFiles embed.FS

// glyph returns an icon of the set by its file name, as "f7-gear-alt".
func glyph(name string) *ui.SVG {
	b, err := iconFiles.ReadFile("icons/" + name + ".svg")
	if err != nil {
		panic("solax: no icon " + name)
	}
	return ui.MustParseSVG(b)
}

var (
	sidebarIcon  = glyph("f7-sidebar-left")
	searchIcon   = glyph("f7-search")
	returnIcon   = glyph("f7-return")
	gearIcon     = glyph("f7-gear-alt")
	reloadIcon   = glyph("f7-arrow-clockwise")
	sparklesIcon = glyph("f7-sparkles")
	jobsIcon     = glyph("f7-list-bullet-indent")
	agentIcon    = glyph("meteor-terminal")
	machineIcon  = glyph("f7-desktopcomputer")

	// The steps of a transcript.
	thoughtIcon   = glyph("f7-lightbulb")
	docIcon       = glyph("f7-doc")
	pencilIcon    = glyph("f7-pencil")
	globeIcon     = glyph("f7-globe")
	toolIcon      = glyph("f7-wrench")
	chevronIcon   = glyph("f7-chevron-right")
	sendIcon      = glyph("f7-arrow-up")
	arrowDownIcon = glyph("f7-arrow-down")
)
