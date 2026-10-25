//go:build unit

package admin

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/stretchr/testify/require"
)

// Storage-boundary validation for admin-authored rich content (audit Q3/Q9).
//
// The frontend sanitizes at render time, but stored configuration is a trust
// boundary: any future render site that forgets to sanitize would turn a stored
// payload into XSS. These tests pin the write path, so a payload that would be
// dangerous at render time is rejected with 400 and never persisted.

func menuItemWithIcon(icon string) map[string]any {
	return map[string]any{
		"custom_menu_items": []map[string]any{{
			"id":         "docs",
			"label":      "Docs",
			"url":        "https://example.com/docs",
			"icon_svg":   icon,
			"visibility": "user",
			"sort_order": 0,
		}},
	}
}

func TestUpdateSettingsRejectsScriptInMenuIconSVG(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, menuItemWithIcon(`<svg><script>alert(1)</script><path d="M0 0h1"/></svg>`), nil)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Nil(t, repo.lastUpdates, "a rejected icon must not be persisted")
}

func TestUpdateSettingsRejectsEventHandlerInMenuIconSVG(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, menuItemWithIcon(`<svg onload="alert(1)"><path d="M0 0h1"/></svg>`), nil)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Nil(t, repo.lastUpdates)
}

func TestUpdateSettingsRejectsForeignObjectInMenuIconSVG(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, menuItemWithIcon(
		`<svg><foreignObject><div xmlns="http://www.w3.org/1999/xhtml">x</div></foreignObject></svg>`), nil)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Nil(t, repo.lastUpdates)
}

func TestUpdateSettingsRejectsJavascriptLinkInMenuIconSVG(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, menuItemWithIcon(
		`<svg><a xlink:href="javascript:alert(1)"><path d="M0 0h1"/></a></svg>`), nil)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Nil(t, repo.lastUpdates)
}

func TestUpdateSettingsAcceptsSafeMenuIconSVG(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	icon := `<svg fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5"><path stroke-linecap="round" d="M2.25 8.25h19.5"/></svg>`

	rec := doUpdateSettings(t, h, menuItemWithIcon(icon), nil)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, repo.values[service.SettingKeyCustomMenuItems], "stroke-linecap")
}

func TestUpdateSettingsRejectsOversizedMenuIconSVG(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	icon := `<svg><path d="` + strings.Repeat("M0 0h1 ", 2000) + `"/></svg>`
	require.Greater(t, len(icon), 10*1024)

	rec := doUpdateSettings(t, h, menuItemWithIcon(icon), nil)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Nil(t, repo.lastUpdates)
}

func TestUpdateSettingsRejectsScriptInHomeContent(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{
		"home_content": `<h1>Welcome</h1><script>alert(document.cookie)</script>`,
	}, nil)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Nil(t, repo.lastUpdates)
}

func TestUpdateSettingsRejectsEventHandlerInHomeContent(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{
		"home_content": `<img src="https://example.com/x.png" onerror="alert(1)">`,
	}, nil)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Nil(t, repo.lastUpdates)
}

func TestUpdateSettingsRejectsJavascriptHrefInHomeContent(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{
		"home_content": `<p><a href="javascript:alert(1)">click</a></p>`,
	}, nil)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Nil(t, repo.lastUpdates)
}

func TestUpdateSettingsRejectsIframeInHomeContent(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{
		"home_content": `<iframe src="https://evil.example"></iframe>`,
	}, nil)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Nil(t, repo.lastUpdates)
}

func TestUpdateSettingsAcceptsSafeHomeContentHTML(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	content := `<section><h1>Welcome</h1><p>Use <strong>Sub2API</strong>.</p>` +
		`<ul><li>Fast</li><li>Reliable</li></ul>` +
		`<p><a href="https://example.com/docs" target="_blank" rel="noopener noreferrer">Docs</a></p>` +
		`<svg viewBox="0 0 24 24"><path d="M0 0h1"/></svg></section>`

	rec := doUpdateSettings(t, h, map[string]any{"home_content": content}, nil)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, repo.values[service.SettingKeyHomeContent], "<h1>Welcome</h1>")
}

func TestUpdateSettingsAcceptsHomeContentURL(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{
		"home_content": "https://example.com/embedded-home",
	}, nil)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "https://example.com/embedded-home", repo.values[service.SettingKeyHomeContent])
}

func TestUpdateSettingsRejectsNonHTTPSchemeHomeContentURL(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{
		"home_content": "javascript:alert(1)",
	}, nil)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Nil(t, repo.lastUpdates)
}

func TestUpdateSettingsRejectsOversizedHomeContent(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	content := "<p>" + strings.Repeat("x", 70*1024) + "</p>"

	rec := doUpdateSettings(t, h, map[string]any{"home_content": content}, nil)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Nil(t, repo.lastUpdates)
}

func TestUpdateSettingsRejectsMalformedHomeContentMarkup(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{
		"home_content": `<p>hello</div>`,
	}, nil)

	require.Equal(t, http.StatusBadRequest, rec.Code, "unbalanced markup must not be silently repaired into stored HTML")
	require.Nil(t, repo.lastUpdates)
}
