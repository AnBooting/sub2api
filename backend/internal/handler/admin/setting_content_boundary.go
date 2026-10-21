package admin

// Storage-boundary validation for admin-authored rich content (audit Q3/Q9).
//
// The frontend already sanitizes SVG at render time (frontend/src/utils/sanitize.ts,
// DOMPurify svg/svgFilters profile). Stored configuration is still a trust boundary:
// a future render site that forgets to sanitize would turn whatever is stored into
// XSS, and a hostile payload would live in the settings table indefinitely.
//
// These validators therefore run on write and fail closed: anything outside the
// allowlist below is rejected with 400 instead of being persisted. The allowlist is
// deliberately a subset of what the render-side sanitizer keeps, so stored content
// can always be rendered safely by that sanitizer as well.

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	// maxMenuIconSVGBytes mirrors the existing custom-menu icon cap.
	maxMenuIconSVGBytes = 10 * 1024
	// maxHomeContentBytes bounds one admin-authored home page document. It is far
	// below the server request-body limit; the point is to stop unbounded storage.
	maxHomeContentBytes = 64 * 1024

	svgNamespace = "svg"
)

// svgAllowedElements is the safe SVG subset for icons embedded in stored settings.
// Interactive/scripting/foreign-content elements are absent on purpose.
var svgAllowedElements = map[string]struct{}{
	"svg": {}, "g": {}, "path": {}, "circle": {}, "rect": {}, "line": {},
	"polyline": {}, "polygon": {}, "ellipse": {}, "defs": {}, "title": {},
	"desc": {}, "clippath": {}, "mask": {}, "lineargradient": {},
	"radialgradient": {}, "stop": {}, "filter": {}, "fegaussianblur": {},
	"feoffset": {}, "feblend": {}, "fecolormatrix": {}, "femerge": {},
	"femergenode": {}, "feflood": {}, "fecomposite": {}, "fedropshadow": {},
}

// svgAllowedAttributes is the presentation/geometry subset used by the icons this
// project ships (see migrations/099) and by the frontend sanitizer's svg profile.
// Event handlers (on*) and href are never allowed.
var svgAllowedAttributes = map[string]struct{}{
	"xmlns": {}, "xmlns:xlink": {}, "version": {}, "viewbox": {},
	"preserveaspectratio": {}, "width": {}, "height": {}, "x": {}, "y": {},
	"x1": {}, "x2": {}, "y1": {}, "y2": {}, "cx": {}, "cy": {}, "r": {},
	"rx": {}, "ry": {}, "d": {}, "points": {}, "transform": {},
	"fill": {}, "fill-opacity": {}, "fill-rule": {}, "stroke": {},
	"stroke-width": {}, "stroke-linecap": {}, "stroke-linejoin": {},
	"stroke-dasharray": {}, "stroke-dashoffset": {}, "stroke-opacity": {},
	"stroke-miterlimit": {}, "opacity": {}, "color": {}, "class": {},
	"id": {}, "style": {}, "offset": {}, "stop-color": {}, "stop-opacity": {},
	"gradientunits": {}, "gradienttransform": {}, "clippathunits": {},
	"maskunits": {}, "maskcontentunits": {}, "filterunits": {},
	"stddeviation": {}, "in": {}, "in2": {}, "result": {}, "mode": {},
	"values": {}, "type": {}, "dx": {}, "dy": {}, "flood-color": {},
	"flood-opacity": {}, "operator": {}, "k1": {}, "k2": {}, "k3": {}, "k4": {},
	"color-interpolation-filters": {}, "aria-hidden": {}, "focusable": {},
	"role": {},
}

// htmlAllowedElements is the safe HTML subset for home_content. Scripting,
// embedding and form elements are absent on purpose; svg children are validated
// against the SVG allowlist instead.
var htmlAllowedElements = map[string]struct{}{
	"p": {}, "br": {}, "hr": {}, "div": {}, "span": {}, "section": {},
	"article": {}, "header": {}, "footer": {}, "main": {}, "aside": {},
	"nav": {}, "h1": {}, "h2": {}, "h3": {}, "h4": {}, "h5": {}, "h6": {},
	"strong": {}, "b": {}, "em": {}, "i": {}, "u": {}, "s": {}, "del": {},
	"ins": {}, "mark": {}, "small": {}, "sub": {}, "sup": {}, "code": {},
	"pre": {}, "blockquote": {}, "ul": {}, "ol": {}, "li": {}, "dl": {},
	"dt": {}, "dd": {}, "table": {}, "thead": {}, "tbody": {}, "tfoot": {},
	"tr": {}, "th": {}, "td": {}, "caption": {}, "figure": {}, "figcaption": {},
	"details": {}, "summary": {}, "a": {}, "img": {}, "picture": {},
	"source": {}, "video": {}, "audio": {},
}

var htmlAllowedAttributes = map[string]struct{}{
	"class": {}, "id": {}, "title": {}, "role": {}, "style": {},
	"alt": {}, "width": {}, "height": {}, "loading": {}, "decoding": {},
	"controls": {}, "loop": {}, "muted": {}, "preload": {}, "poster": {},
	"colspan": {}, "rowspan": {}, "scope": {}, "start": {}, "reversed": {},
	"target": {}, "rel": {}, "download": {}, "type": {},
	// URL-bearing attributes are allowed but their value must pass
	// validateContentURLAttribute (http/https/mailto/tel or a relative reference).
	"href": {}, "src": {},
}

// urlAttributes carry a URL and must pass scheme validation.
var urlAttributes = map[string]struct{}{"href": {}, "src": {}, "poster": {}, "xlink:href": {}}

// styleForbiddenFragments blocks CSS vectors that can load or execute code even
// though the style attribute itself is only text.
var styleForbiddenFragments = []string{
	"url(", "expression", "javascript:", "vbscript:", "@import", "behavior:", "-moz-binding",
}

// validateMenuIconSVG validates one custom-menu icon. Empty means "no icon".
func validateMenuIconSVG(icon string) error {
	if icon == "" {
		return nil
	}
	if len(icon) > maxMenuIconSVGBytes {
		return fmt.Errorf("icon SVG is too large (max %d bytes)", maxMenuIconSVGBytes)
	}
	context := &html.Node{Type: html.ElementNode, Data: "svg", DataAtom: atom.Svg, Namespace: svgNamespace}
	nodes, err := html.ParseFragment(strings.NewReader(icon), context)
	if err != nil {
		return fmt.Errorf("icon SVG is not valid markup: %w", err)
	}
	for _, node := range nodes {
		if err := validateSVGNode(node); err != nil {
			return err
		}
	}
	return nil
}

func validateSVGNode(node *html.Node) error {
	if node == nil {
		return nil
	}
	switch node.Type {
	case html.CommentNode, html.TextNode:
		return nil
	case html.ElementNode:
		name := strings.ToLower(node.Data)
		if _, ok := svgAllowedElements[name]; !ok {
			return fmt.Errorf("icon SVG element <%s> is not allowed", node.Data)
		}
		if node.Namespace != svgNamespace {
			return fmt.Errorf("icon SVG element <%s> is outside the SVG namespace", node.Data)
		}
		if err := validateSVGAttributes(node); err != nil {
			return err
		}
	default:
		return fmt.Errorf("icon SVG contains unsupported markup")
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if err := validateSVGNode(child); err != nil {
			return err
		}
	}
	return nil
}

func validateSVGAttributes(node *html.Node) error {
	for _, attr := range node.Attr {
		name := strings.ToLower(attr.Key)
		if attr.Namespace != "" {
			name = strings.ToLower(attr.Namespace) + ":" + name
		}
		if strings.HasPrefix(name, "on") {
			return fmt.Errorf("icon SVG attribute %q is not allowed", attr.Key)
		}
		if _, ok := svgAllowedAttributes[name]; !ok {
			return fmt.Errorf("icon SVG attribute %q is not allowed", attr.Key)
		}
		if _, isURL := urlAttributes[name]; isURL {
			return fmt.Errorf("icon SVG attribute %q is not allowed", attr.Key)
		}
		if name == "style" && !safeInlineStyle(attr.Val) {
			return fmt.Errorf("icon SVG style contains a forbidden construct")
		}
	}
	return nil
}

// validateHomeContent validates the admin home page payload. A bare http(s) URL is
// the documented iframe mode; everything else is treated as Markdown/HTML and must
// stay inside the HTML allowlist.
func validateHomeContent(content string) error {
	if len(content) > maxHomeContentBytes {
		return fmt.Errorf("home content is too large (max %d bytes)", maxHomeContentBytes)
	}
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil
	}
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return validateHomeContentURL(trimmed)
	}
	// Any other scheme-looking value would be rendered as text today, but storing a
	// javascript:/data: payload is exactly what the boundary exists to prevent.
	if scheme, ok := leadingURLScheme(trimmed); ok {
		return fmt.Errorf("home content URL scheme %q is not allowed", scheme)
	}
	if !strings.Contains(trimmed, "<") {
		// Plain Markdown/text has no markup to validate.
		return nil
	}
	if err := validateMarkupBalance(trimmed); err != nil {
		return err
	}
	context := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(trimmed), context)
	if err != nil {
		return fmt.Errorf("home content is not valid markup: %w", err)
	}
	for _, node := range nodes {
		if err := validateHTMLNode(node); err != nil {
			return err
		}
	}
	return nil
}

// htmlVoidElements never take a closing tag, so the balance check must not
// expect one for them.
var htmlVoidElements = map[string]struct{}{
	"area": {}, "base": {}, "br": {}, "col": {}, "embed": {}, "hr": {},
	"img": {}, "input": {}, "link": {}, "meta": {}, "param": {},
	"source": {}, "track": {}, "wbr": {},
}

// validateMarkupBalance rejects markup the HTML parser would have to silently
// repair. The allowlist walk runs on the repaired tree, so a payload such as
// "<p>hello</div>" would otherwise be accepted and stored in a shape the admin
// never wrote. Tokenizing the raw input keeps the stored value honest.
func validateMarkupBalance(content string) error {
	tokenizer := html.NewTokenizer(strings.NewReader(content))
	var stack []string
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			if err := tokenizer.Err(); err != nil && !errors.Is(err, io.EOF) {
				return fmt.Errorf("home content is not valid markup: %w", err)
			}
			if len(stack) > 0 {
				return fmt.Errorf("home content has an unclosed <%s> element", stack[len(stack)-1])
			}
			return nil
		case html.StartTagToken:
			name, _ := tokenizer.TagName()
			element := strings.ToLower(string(name))
			if _, isVoid := htmlVoidElements[element]; isVoid {
				continue
			}
			stack = append(stack, element)
		case html.SelfClosingTagToken:
			continue
		case html.EndTagToken:
			name, _ := tokenizer.TagName()
			element := strings.ToLower(string(name))
			if _, isVoid := htmlVoidElements[element]; isVoid {
				return fmt.Errorf("home content has an unexpected </%s>", element)
			}
			if len(stack) == 0 || stack[len(stack)-1] != element {
				return fmt.Errorf("home content has a mismatched </%s>", element)
			}
			stack = stack[:len(stack)-1]
		}
	}
}

func validateHomeContentURL(raw string) error {
	if err := validateHTTPURL(raw); err != nil {
		return fmt.Errorf("home content URL is invalid: %w", err)
	}
	return nil
}

func validateHTTPURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return err
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("unsupported scheme %q", parsed.Scheme)
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return fmt.Errorf("missing host")
	}
	return nil
}

// leadingURLScheme reports a URL scheme prefix like "javascript:" without requiring
// the value to parse as an absolute URL.
func leadingURLScheme(value string) (string, bool) {
	colon := strings.IndexByte(value, ':')
	if colon <= 0 {
		return "", false
	}
	scheme := value[:colon]
	for i, r := range scheme {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case i > 0 && r >= '0' && r <= '9', i > 0 && (r == '+' || r == '-' || r == '.'):
		default:
			return "", false
		}
	}
	return strings.ToLower(scheme), true
}

func validateHTMLNode(node *html.Node) error {
	if node == nil {
		return nil
	}
	switch node.Type {
	case html.CommentNode, html.TextNode:
		return nil
	case html.ElementNode:
		name := strings.ToLower(node.Data)
		if node.Namespace == svgNamespace {
			return validateSVGNode(node)
		}
		if node.Namespace != "" {
			return fmt.Errorf("home content element <%s> is outside the HTML namespace", node.Data)
		}
		if _, ok := htmlAllowedElements[name]; !ok {
			return fmt.Errorf("home content element <%s> is not allowed", node.Data)
		}
		if err := validateHTMLAttributes(node); err != nil {
			return err
		}
	default:
		return fmt.Errorf("home content contains unsupported markup")
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if err := validateHTMLNode(child); err != nil {
			return err
		}
	}
	return nil
}

func validateHTMLAttributes(node *html.Node) error {
	for _, attr := range node.Attr {
		name := strings.ToLower(attr.Key)
		if strings.HasPrefix(name, "on") {
			return fmt.Errorf("home content attribute %q is not allowed", attr.Key)
		}
		if strings.HasPrefix(name, "aria-") || strings.HasPrefix(name, "data-") {
			continue
		}
		if _, ok := htmlAllowedAttributes[name]; !ok {
			return fmt.Errorf("home content attribute %q is not allowed", attr.Key)
		}
		if _, isURL := urlAttributes[name]; isURL {
			if err := validateContentURLAttribute(attr.Val); err != nil {
				return err
			}
		}
		if name == "style" && !safeInlineStyle(attr.Val) {
			return fmt.Errorf("home content style contains a forbidden construct")
		}
	}
	return nil
}

// validateContentURLAttribute allows absolute http(s)/mailto/tel URLs and relative
// references. Everything else (javascript:, data:, vbscript:, ...) is rejected.
func validateContentURLAttribute(raw string) error {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil
	}
	if strings.HasPrefix(value, "#") || strings.HasPrefix(value, "/") || strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../") {
		return nil
	}
	scheme, ok := leadingURLScheme(value)
	if !ok {
		// Relative reference such as "docs/intro".
		return nil
	}
	switch scheme {
	case "http", "https", "mailto", "tel":
		return nil
	default:
		return fmt.Errorf("URL scheme %q is not allowed", scheme)
	}
}

func safeInlineStyle(style string) bool {
	lower := strings.ToLower(style)
	for _, fragment := range styleForbiddenFragments {
		if strings.Contains(lower, fragment) {
			return false
		}
	}
	return true
}
