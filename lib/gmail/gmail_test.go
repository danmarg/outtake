package gmail

import (
	"encoding/base64"
	"errors"
	"github.com/danmarg/outtake/lib"
	"github.com/danmarg/outtake/lib/maildir"
	gmail "google.golang.org/api/gmail/v1"
	"io/ioutil"
	"os"
	"path"
	"sort"
	"strings"
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

type testService struct {
	gmailService
	Msgs     map[string]string
	Metadata map[string]*gmail.Message
	Labels   *gmail.ListLabelsResponse
	History  map[string]*gmail.ListHistoryResponse
	Messages map[string]*gmail.ListMessagesResponse
}

func (s *testService) GetRawMessage(id string) (string, error) {
	if m, ok := s.Msgs[id]; ok {
		return m, nil
	}
	return "", errors.New("not found")
}

func (s *testService) GetMetadata(id string) (*gmail.Message, error) {
	if m, ok := s.Metadata[id]; ok {
		return m, nil
	}
	return nil, errors.New("not found")
}

func (s *testService) GetLabels() (*gmail.ListLabelsResponse, error) {
	return s.Labels, nil
}

func (s *testService) GetHistory(i uint64, label, page string) (*gmail.ListHistoryResponse, error) {
	if m, ok := s.History[page]; ok {
		return m, nil
	}
	return nil, errors.New("not found")
}

func (s *testService) GetMessages(q, page string) (*gmail.ListMessagesResponse, error) {
	if m, ok := s.Messages[page]; ok {
		return m, nil
	}
	return nil, errors.New("not found")
}

func getTestClient() (*Gmail, *testService, string) {
	d, err := ioutil.TempDir("", "")
	if err != nil {
		panic(err)
	}
	var c lib.Cache
	if c, err = lib.NewBoltCache(d + "test_cache"); err != nil {
		panic(err)
	}
	md, err := maildir.Create(d)
	if err != nil {
		panic(err)
	}
	s := &testService{
		Msgs:     make(map[string]string),
		Metadata: make(map[string]*gmail.Message),
		Messages: make(map[string]*gmail.ListMessagesResponse),
		History:  make(map[string]*gmail.ListHistoryResponse),
	}
	g := &Gmail{
		dir:   md,
		cache: gmailCache{c},
		svc:   s,
	}
	return g, s, d
}

func TestSync(t *testing.T) {
	c, svc, dir := getTestClient()
	m := base64.URLEncoding.EncodeToString([]byte(
		`From: billg@microsoft.com
To: page@google.com
Subject: Doodle!

asdf`))
	svc.Msgs["0x1"], svc.Msgs["0x2"], svc.Msgs["0x3"] = m, m, m
	svc.Metadata["0x1"], svc.Metadata["0x2"], svc.Metadata["0x3"] = &gmail.Message{}, &gmail.Message{}, &gmail.Message{}
	svc.Labels = &gmail.ListLabelsResponse{}
	svc.Messages[""] = &gmail.ListMessagesResponse{
		Messages: []*gmail.Message{
			{Id: "0x1"},
			{Id: "0x2"},
			{Id: "0x3"}},
	}
	svc.Metadata["0x1"] = &gmail.Message{Id: "0x01", HistoryId: 1}
	svc.Metadata["0x2"] = &gmail.Message{Id: "0x02", HistoryId: 2}
	svc.Metadata["0x3"] = &gmail.Message{Id: "0x03", HistoryId: 3, LabelIds: []string{"LABEL_3"}}
	err := c.Sync(false, nil)
	if err != nil {
		t.Errorf(`Sync(false, nil) = %v, expected nil`, err)
	}
	// There should be three new messages in the maildir.
	fs, err := ioutil.ReadDir(dir + "/new")
	if err != nil {
		panic(err)
	}
	if len(fs) != 3 {
		t.Errorf(`Sync(true, nil) wrote %v messages, expected 3`, len(fs))
	}
	if i := c.cache.GetHistoryIdx(); i != 3 {
		t.Errorf(`GetHistoryIdx() == %v, expected 3`, i)
	}
	// And one of the messages should have LABEL_3 set.
	k, ok := c.cache.GetMsgKey("0x3")
	if !ok {
		t.Errorf(`GetMsgKey("0x3") == false, expected true`)
	}
	f, err := c.dir.GetFile(k)
	if err != nil {
		t.Errorf(`GetFile(%v) == %v, expected no error`, k, err)
	}
	bs, err := ioutil.ReadFile(f)
	if err != nil {
		t.Errorf(`ReadFile(%v) == %v, expected no error`, f, err)
	}
	// Check contents of bs.
	if !strings.Contains(string(bs), "X-Keywords: LABEL_3") {
		t.Errorf(`Expected %v to contain X-Keywords: LABEL_3`, string(bs))
	}
	// If we move 0x3 to ./cur and add some maildir flags like "Seen", it should still work.
	err = os.Rename(f, dir+"/cur/"+path.Base(f)+":S")
	if err != nil {
		panic(err)
	}
	// Message sync: delete 0x1, add a label to 0x2, remove LABEL_3 from 0x3, and add message 0x4.
	svc.History[""] = &gmail.ListHistoryResponse{
		History: []*gmail.History{
			{
				Id:              1,
				MessagesDeleted: []*gmail.HistoryMessageDeleted{{&gmail.Message{Id: "0x1"}}},
				LabelsAdded:     []*gmail.HistoryLabelAdded{{LabelIds: []string{"LABEL_2"}, Message: &gmail.Message{Id: "0x2"}}},
				LabelsRemoved:   []*gmail.HistoryLabelRemoved{{LabelIds: []string{"LABEL_3"}, Message: &gmail.Message{Id: "0x3"}}},
				MessagesAdded:   []*gmail.HistoryMessageAdded{{&gmail.Message{Id: "0x4"}}},
			},
		},
	}
	// Add the new message 0x4 body.
	svc.Msgs["0x4"] = m
	// And metadata.
	svc.Metadata["0x4"] = &gmail.Message{}
	err = c.Sync(false, nil)
	if err != nil {
		t.Errorf(`Sync(false, nil) = %v, expected nil`, err)
	}
	// There should be two new messages in the maildir.
	fs, err = ioutil.ReadDir(dir + "/new")
	if err != nil {
		panic(err)
	}
	if len(fs) != 3 {
		t.Errorf(`Sync(true, nil) wrote %v messages to "new", expected 3`, len(fs))
	}
	// And zero in "cur".
	fs, err = ioutil.ReadDir(dir + "/cur")
	if err != nil {
		panic(err)
	}
	if len(fs) != 0 {
		t.Errorf(`Sync(true, nil) wrote %v messages to "cur", expected 0`, len(fs))
	}
	// And 0x3 should no longer have LABEL_3 set.
	k, ok = c.cache.GetMsgKey("0x3")
	if !ok {
		t.Errorf(`GetMsgKey("0x3") == false, expected true`)
	}
	f, err = c.dir.GetFile(k)
	if err != nil {
		t.Errorf(`GetFile(%v) == %v, expected no error`, k, err)
	}
	bs, err = ioutil.ReadFile(f)
	if err != nil {
		t.Errorf(`ReadFile(%v) == %v, expected no error`, f, err)
	}
	// Check contents of bs.
	if strings.Contains(string(bs), "X-Keywords: LABEL_3") {
		t.Errorf(`Expected %v to not contain X-Keywords: LABEL_3`, string(bs))
	}
	// And 0x2 should have LABEL_2 set.
	k, ok = c.cache.GetMsgKey("0x2")
	if !ok {
		t.Errorf(`GetMsgKey("0x2") == false, expected true`)
	}
	f, err = c.dir.GetFile(k)
	if err != nil {
		t.Errorf(`GetFile(%v) == %v, expected no error`, k, err)
	}
	bs, err = ioutil.ReadFile(f)
	if err != nil {
		t.Errorf(`ReadFile(%v) == %v, expected no error`, f, err)
	}
	// Check contents of bs.
	if !strings.Contains(string(bs), "X-Keywords: LABEL_2") {
		t.Errorf(`Expected %v to contain X-Keywords: LABEL_2`, string(bs))
	}
}
