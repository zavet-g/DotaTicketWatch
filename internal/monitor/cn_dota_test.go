package monitor

import (
	"strings"
	"testing"
)

func TestExtractCNHeadlines_RealStructure(t *testing.T) {
	html := `<a href="article/details/20260506/220471.html"><div>...
<i class="title">一京两沪一秦川！刀塔校友会校友赛道四强出炉</i></a>
<a href="article/details/20260417/220465.html"><i class="title">报名参赛刀塔校友会，抽取TI2026现场观赛门票！</i></a>
<a href="article/details/20260325/220463.html"><i class="title">7.41游戏性更新</i></a>`

	got := ExtractCNHeadlines(html)
	if len(got) != 3 {
		t.Fatalf("want 3 headlines, got %d: %+v", len(got), got)
	}
	if got[0].ArticleID != "220471" || got[0].Date != "20260506" {
		t.Errorf("first: %+v", got[0])
	}
	if !strings.Contains(got[1].Title, "TI2026") {
		t.Errorf("second title: %s", got[1].Title)
	}
	if got[2].URL != "https://www.dota2.com.cn/article/details/20260325/220463.html" {
		t.Errorf("url: %s", got[2].URL)
	}
}

func TestExtractCNHeadlines_DeduplicatesByID(t *testing.T) {
	html := `<a href="article/details/20260506/220471.html"><i class="title">first occurrence</i></a>
<a href="article/details/20260506/220471.html"><i class="title">duplicate</i></a>`
	got := ExtractCNHeadlines(html)
	if len(got) != 1 {
		t.Fatalf("want 1 (dedup), got %d", len(got))
	}
}
