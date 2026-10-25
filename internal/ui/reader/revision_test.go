package reader

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/client"
)

func TestDiffParagraphs(t *testing.T) {
	before := client.RevisionVersion{Title: "Go  1.27", Paragraphs: []string{"a", "b", "c", "d"}}
	now := client.RevisionVersion{Title: "Go 1.27.1", Paragraphs: []string{"a", "c", "x", "d"}}
	got := diffParagraphs(before, now)
	want := []change{
		{kind: removed, text: "Go 1.27", title: true}, {kind: added, text: "Go 1.27.1", title: true},
		{kind: kept, n: 1}, {kind: removed, text: "b"}, {kind: kept, n: 1}, {kind: added, text: "x"}, {kind: kept, n: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	if got := diffParagraphs(client.RevisionVersion{Paragraphs: []string{"a", "b"}}, client.RevisionVersion{Paragraphs: []string{"a", "b"}}); !reflect.DeepEqual(got, []change{{kind: kept, n: 2}}) {
		t.Fatalf("unchanged: %+v", got)
	}
}

func revised() client.Article {
	a := full()
	a.RevisedAt = now.Add(-2 * time.Hour).UnixMilli()
	return a
}

func TestRevisedNoticeShowsChanges(t *testing.T) {
	for _, c := range []struct {
		lang                               string
		notice, show, hide, removed, added string
	}{
		{"en", "Updated ", "See changes", "Hide changes", "Removed", "Added"},
		{"zh-Hans", "已于 ", "查看改动", "收起改动", "删除", "新增"},
	} {
		t.Run(c.lang, func(t *testing.T) {
			article := revised()
			s, f, tt := setup(t, c.lang, article)
			f.revision = client.Revision{
				Previous: client.RevisionVersion{Title: "Go 1.27 is released", Paragraphs: []string{"One.", "Old paragraph.", "Three."}},
				Current:  client.RevisionVersion{Title: "Go 1.27 is released", Paragraphs: []string{"One.", "New paragraph.", "Three."}},
			}
			f.settle(tt)
			if !s.rev.ready || len(s.rev.changes) != 4 {
				t.Fatalf("changes %+v", s.rev)
			}
			expect(t, tt, c.show)
			absent(t, tt, "Old paragraph.", "New paragraph.")
			click(t, tt, c.show)
			tt.Frame()
			expect(t, tt, c.hide, "Old paragraph.", "New paragraph.", c.removed, c.added)
			click(t, tt, c.hide)
			tt.Frame()
			absent(t, tt, "Old paragraph.")
		})
	}
}

func TestNoRevisedNoticeForUnchangedArticle(t *testing.T) {
	_, f, tt := setup(t, "en", full())
	f.settle(tt)
	absent(t, tt, "See changes")
	if f.ctxs["revision"] != nil {
		t.Fatal("asked for a revision of an entry never updated")
	}
}

func TestRevisedNoticeWithoutRevision(t *testing.T) {
	_, f, tt := setup(t, "en", revised())
	f.revisionErr = errors.New("gone")
	f.settle(tt)
	expect(t, tt, "Updated Oct 18, 10:00 AM")
	absent(t, tt, "See changes")
}

func TestRevisionDroppedWhenArticleChanges(t *testing.T) {
	s, f, tt := setup(t, "en", revised())
	f.revision = client.Revision{Previous: client.RevisionVersion{Paragraphs: []string{"a"}}, Current: client.RevisionVersion{Paragraphs: []string{"b"}}}
	s.Open(client.Entry{ID: 9, Title: "Other"})
	f.settle(tt)
	if s.rev.ready {
		t.Fatalf("revision of the entry before shows: %+v", s.rev)
	}
}
