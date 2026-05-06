package monitor

import (
	"strings"
	"testing"
)

func TestSanitizeAXSHTML_KeepsNextDataDropsOthers(t *testing.T) {
	html := `<html><head>
<style>body{font:0}</style>
<script src="aos.js"></script>
<script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{}}}</script>
</head><body><img src="data:image/png;base64,AAAA"/>hello</body></html>`

	out := sanitizeAXSHTML(html)
	if !strings.Contains(out, "__NEXT_DATA__") {
		t.Errorf("__NEXT_DATA__ must survive: %s", out)
	}
	if strings.Contains(out, "aos.js") {
		t.Errorf("normal script must be removed: %s", out)
	}
	if strings.Contains(out, "body{font:0}") {
		t.Errorf("style must be removed: %s", out)
	}
	if strings.Contains(out, "data:image/png;base64,AAAA") {
		t.Errorf("data URI must be stripped: %s", out)
	}
}

func TestSanitizeAXSHTML_TruncatesAt60kb(t *testing.T) {
	huge := strings.Repeat("x", axsHTMLMaxLen+1000)
	out := sanitizeAXSHTML(huge)
	if len(out) > axsHTMLMaxLen {
		t.Errorf("not truncated: %d", len(out))
	}
}
