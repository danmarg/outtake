package gmail

import (
	"github.com/danmarg/outtake/lib"
	"io/ioutil"
	"path"
	"sort"
	"testing"
)

func newTestCache() gmailCache {
	d, err := ioutil.TempDir("", "")
	if err != nil {
		panic(err)
	}
	f := path.Join(d, "test_outtake_cache")
	if c, err := lib.NewBoltCache(f); err != nil {
		panic(err)
	} else {
		return gmailCache{c}
	}
}

func TestComputeLabels(t *testing.T) {
	g := Gmail{cache: newTestCache()}
	g.cache.SetMsgLabels("id", []string{"a", "b"})
	ls := g.computeLabels("id", []string{"c"}, []string{"b"})
	sort.Strings(ls)
	if len(ls) != 2 || ls[0] != "a" || ls[1] != "c" {
		t.Errorf(`computeLabels("id", {"c"}, {"b"}) = %v, expected {"a", "c"}`, ls)
	}
}

func TestLabelsChanged(t *testing.T) {
	g := Gmail{cache: newTestCache()}
	g.cache.SetMsgLabels("id", []string{"a", "b"})
	if !g.labelsChanged("id", []string{"a"}) {
		t.Error(`labelsChanged("id", {"a"}) = false, expected true`)
	}
	if g.labelsChanged("id", []string{"a", "b"}) {
		t.Error(`labelsChanged("id", {"a", "b"}) = true, expected false`)
	}
	if !g.labelsChanged("id", []string{}) {
		t.Error(`labelsChanged("id", {}) = false, expected true`)
	}
	if !g.labelsChanged("id", []string{"a", "b", "c"}) {
		t.Error(`labelsChanged("id", {"a", "b", "c"}) = false, expected true`)
	}
}
