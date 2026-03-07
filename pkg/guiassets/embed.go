package guiassets

import (
	_ "embed"
	"strings"
)

//go:embed assets/index.html
var htmlData string

//go:embed assets/style.css
var cssData string

//go:embed assets/app.js
var jsData string

// ComposeHTML returns the final HTML with CSS and JS injected.
func ComposeHTML() string {
	html := strings.Replace(htmlData, "/* INJECT_CSS */", cssData, 1)
	html = strings.Replace(html, "/* INJECT_JS */", jsData, 1)
	return html
}
