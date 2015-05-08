package gmail

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"log"
	"net/mail"
	"os"
	"path"
	"sort"
	"sync"
	"time"

	"github.com/danmarg/outtake/lib"
	"github.com/danmarg/outtake/lib/maildir"
	"github.com/danmarg/outtake/lib/oauth"
	gmail "github.com/google/google-api-go-client/gmail/v1"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
)

const (
	// What X- header to use for storing labels.
	labelsHeader = "X-Keywords"
	// Cache filename.
	cacheFile = ".outtake"
	// Parallelism.
	messageBufferSize   = 128
	concurrentDownloads = 8
	maxQps              = 240
)

var (
	// Errors.
	unknownMessage   = errors.New("unknown message")
	alreadyExists    = errors.New("already exists")
	fullSyncRequired = errors.New("full sync required")
)

type gmailCache struct {
	Cache Cache
}

func (c *gmailCache) GetOauthToken() (*oauth2.Token, bool) {
	var tok oauth2.Token
	if bs, ok := c.Cache.Get(oauthToken, "0"); ok {
		if err := gob.NewDecoder(bytes.NewBuffer(bs)).Decode(&tok); err != nil {
			panic(err)
		}
		return &tok, true
	}
	return nil, false
}

func (c *gmailCache) SetOauthToken(tok *oauth2.Token) {
	bs := new(bytes.Buffer)
	if err := gob.NewEncoder(bs).Encode(tok); err != nil {
		panic(err)
	}
	c.Cache.Set(oauthToken, "0", bs.Bytes())
}

func (c *gmailCache) GetMsgKey(m string) (maildir.Key, bool) {
	k, ok := c.Cache.Get(midToKey, m)
	return maildir.Key(k), ok
}

func (c *gmailCache) SetMsgKey(m string, k maildir.Key) {
	c.Cache.Set(midToKey, m, []byte(k))
}

func (g *gmailCache) GetMsgs(ms chan<- string) {
	g.Cache.Items(midToKey, ms)
}

func (c *gmailCache) DelMsg(m string) {
	c.Cache.Del(midToKey, m)
	c.Cache.Del(midToLabels, m)
}

func (c *gmailCache) GetMsgLabels(m string) ([]string, bool) {
	ls := []string{}
	bls, ok := c.Cache.Get(midToLabels, m)
	if !ok {
		return ls, false
	}
	if err := gob.NewDecoder(bytes.NewBuffer(bls)).Decode(&ls); err != nil {
		panic(err)
	}
	return ls, ok
}

func (c *gmailCache) SetMsgLabels(m string, ls []string) {
	bls := new(bytes.Buffer)
	if err := gob.NewEncoder(bls).Encode(ls); err != nil {
		panic(err)
	}
	c.Cache.Set(midToLabels, m, bls.Bytes())
}

func (c *gmailCache) GetHistoryIdx() uint64 {
	hidx := uint64(0)
	if b, ok := c.Cache.Get(historyIndex, "0"); ok {
		hidx, _ = binary.Uvarint(b)
	}
	return hidx
}

func (c *gmailCache) SetHistoryIdx(i uint64) {
	b := make([]byte, 8)
	binary.PutUvarint(b, i)
	c.Cache.Set(historyIndex, "0", b)
}

// Gmail represents a Gmail client Maildir.
type Gmail struct {
	label    string
	labelId  string
	cache    gmailCache
	svc      *gmail.UsersService
	dir      maildir.Maildir
	progress chan<- lib.Progress
	limiter  lib.RateLimit
}

// Creates a new Gmail synchronizer.
func NewGmail(dir string, label string) (*Gmail, error) {
	g := Gmail{
		label: label,
	}
	f := path.Join(dir, cacheFile)
	if c, err := lib.NewBoltCache(f); err != nil {
		return nil, err
	} else {
		g.cache = gmailCache{c}
	}
	cfg := &oauth2.Config{
		ClientID:     oauth.ClientId,
		ClientSecret: oauth.Secret,
		Scopes:       []string{gmail.GmailReadonlyScope},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://accounts.google.com/o/oauth2/auth",
			TokenURL: "https://accounts.google.com/o/oauth2/token",
		},
	}
	tok, ok := g.cache.GetOauthToken()
	if !ok {
		// TODO: should we use a client-specified context here?
		var err error
		tok, err = oauth.GetOAuthClient(context.TODO(), cfg)
		if err != nil {
			return nil, err
		}
		g.cache.SetOauthToken(tok)
	}
	clt := cfg.Client(oauth2.NoContext, tok)
	if c, err := gmail.New(clt); err != nil {
		return nil, err
	} else {
		g.svc = gmail.NewUsersService(c)
	}
	g.limiter = lib.RateLimit{Period: time.Second, Rate: maxQps}
	if d, err := maildir.Create(dir); err != nil {
		return nil, err
	} else {
		g.dir = d
	}

	return &g, nil
}

func (g *Gmail) getMaildirMessage(k maildir.Key) (*mail.Message, io.ReadCloser, error) {
	fn, err := g.dir.GetFile(k)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(fn)
	if err != nil {
		return nil, nil, err
	}
	m, err := mail.ReadMessage(f)
	return m, f, err
}

func (g *Gmail) addMessage(m string) error {
	if _, ok := g.cache.GetMsgKey(m); ok {
		// Already exists.
		return alreadyExists
	}
	// Get the message content.
	g.limiter.Get()
	body, err := g.svc.Messages.Get("me", m).Format("raw").Do()
	if err != nil {
		return err
	}
	raw, err := base64.URLEncoding.DecodeString(body.Raw)
	if err != nil {
		return err
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	// Get the message labels.
	g.limiter.Get()
	labels, err := g.svc.Messages.Get("me", m).Format("metadata").Do()
	if err != nil {
		return err
	}
	msg.Header[labelsHeader] = labels.LabelIds
	k, err := g.dir.Deliver(msg)
	if err != nil {
		return err
	}
	// Update the cache.
	g.cache.SetMsgLabels(m, labels.LabelIds)
	g.cache.SetMsgKey(m, k)
	return nil
}

func (g *Gmail) delMessage(m string) error {
	k, ok := g.cache.GetMsgKey(m)
	if !ok {
		return unknownMessage
	}
	if err := g.dir.Delete(k); err != nil {
		return err
	}
	g.cache.DelMsg(m)
	return nil
}

func (g *Gmail) addLabels(m string, labels []string) error {
	k, ok := g.cache.GetMsgKey(m)
	if !ok {
		return unknownMessage
	}
	// Check that the labels actually changed.
	old, ok := g.cache.GetMsgLabels(m)
	if ok {
		sort.Strings(old)
		sort.Strings(labels)
		if old != nil && len(old) == len(labels) {
			eq := true
			for i := 0; i < len(old); i++ {
				if old[i] != labels[i] {
					eq = false
					break
				}
			}
			if eq {
				// No change.
				return nil
			}
		}
	}
	msg, c, err := g.getMaildirMessage(k)
	defer c.Close()
	if err != nil {
		return err
	}
	msg.Header[labelsHeader] = labels
	// Note that this will mark a message as "new" for any clients. This might be undesirable if only labels have changed?
	kn, err := g.dir.Deliver(msg)
	if err != nil {
		return err
	}
	// Update the cache.
	g.cache.SetMsgLabels(m, labels)
	g.cache.SetMsgKey(m, kn)
	// Delete the old message
	if err := g.dir.Delete(k); err != nil {
		return err
	}
	return nil
}

func (g *Gmail) delLabels(m string, labels []string) error {
	k, ok := g.cache.GetMsgKey(m)
	if !ok {
		return unknownMessage
	}
	// Check that the labels actually changed.
	old, ok := g.cache.GetMsgLabels(m)
	if !ok {
		return nil
	}
	nw := make(map[string]struct{})
	for _, l := range old {
		nw[l] = struct{}{}
	}
	for _, l := range labels {
		delete(nw, l)
	}
	if len(nw) == len(old) {
		// No change.
		return nil
	}
	msg, c, err := g.getMaildirMessage(k)
	if err != nil {
		return err
	}
	defer c.Close()
	ls := make([]string, len(nw))
	i := 0
	for l, _ := range nw {
		ls[i] = l
		i++
	}
	msg.Header[labelsHeader] = ls
	// Note that this will mark amessage as "new" for any clients. This might be undesirable if only labels have changed?
	kn, err := g.dir.Deliver(msg)
	if err != nil {
		return err
	}
	// Update the cache.
	g.cache.SetMsgLabels(m, ls)
	g.cache.SetMsgKey(m, kn)
	// Delete the old message
	if err := g.dir.Delete(k); err != nil {
		return err
	}
	return nil
}

func (g *Gmail) setLabels(m string, labels []string) error {
	k, ok := g.cache.GetMsgKey(m)
	if !ok {
		return unknownMessage
	}
	// Check that the labels actually changed.
	old, ok := g.cache.GetMsgLabels(m)
	if !ok {
		return nil
	}
	sort.Strings(old)
	sort.Strings(labels)
	if len(old) == len(labels) {
		changed := false
		for i := 0; i < len(old); i++ {
			if old[i] != labels[i] {
				changed = true
			}
		}
		if !changed {
			return nil
		}
	}
	msg, c, err := g.getMaildirMessage(k)
	if err != nil {
		return err
	}
	defer c.Close()
	msg.Header[labelsHeader] = labels
	// Note that this will mark a message as "new" for any clients. This might be undesirable if only labels have changed?
	k, err = g.dir.Deliver(msg)
	if err != nil {
		return err
	}
	// Update the cache.
	g.cache.SetMsgLabels(m, labels)
	g.cache.SetMsgKey(m, k)
	return nil

}
func (g *Gmail) processFullMessageList(ms <-chan string) (uint64, error) {
	hist := uint64(0)
	for m := range ms {
		if err := g.addMessage(m); err != nil && err != alreadyExists {
			log.Println(err)
			continue
		} else if err == alreadyExists {
			// Just see if the labels need to be updated.
			g.limiter.Get()
			msg, err := g.svc.Messages.Get("me", m).Format("metadata").Do()
			if err != nil {
				log.Println(err)
				continue
			}
			if msg.HistoryId > hist {
				hist = msg.HistoryId
			}
			if err := g.setLabels(m, msg.LabelIds); err != nil {
				log.Println(err)
				continue
			}
		}
	}
	return hist, nil
}

func (g *Gmail) processDeletes(seen map[string]struct{}) error {
	// Now do implicit deletes.
	is := make(chan string)
	go g.cache.GetMsgs(is)
	for m := range is {
		if _, ok := seen[m]; !ok {
			k, ok := g.cache.GetMsgKey(m)
			if !ok {
				return errors.New("cache inconsistency!")
			}
			if err := g.dir.Delete(k); err != nil {
				return err
			}
			g.cache.DelMsg(m)
		}
	}
	return nil
}

func (g *Gmail) labelToId(label string) (string, error) {
	g.limiter.Get()
	ls, err := g.svc.Labels.List("me").Do()
	if err != nil {
		return "", err
	}
	for _, l := range ls.Labels {
		if l.Name == label {
			return l.Id, nil
		}
	}
	return "", errors.New("label not found")
}

func (g *Gmail) incremental(historyId uint64) error {
	log.Println("Performing incremental sync.")
	hist := g.svc.History.List("me").StartHistoryId(historyId)
	if g.labelId != "" {
		hist.LabelId(g.labelId)
	}
	page := ""
	ms := make(chan *gmail.History, messageBufferSize)
	wg := sync.WaitGroup{}
	for i := 0; i < concurrentDownloads; i++ {
		wg.Add(1)
		go func() {
			for h := range ms {
				// Do adds, etc.
				for _, m := range h.MessagesAdded {
					if err := g.addMessage(m.Message.Id); err != nil {
						log.Println("Add message: ", m, err)
					}
				}
				for _, m := range h.MessagesDeleted {
					if err := g.delMessage(m.Message.Id); err != nil && err != unknownMessage {
						// We can get an "unknown message" error if something we never downloaded is deleted--like a draft or a chat.
						log.Println("Delete message: ", m, err)
					}
				}
				for _, m := range h.LabelsAdded {
					if err := g.addLabels(m.Message.Id, m.LabelIds); err != nil {
						log.Println("Add label: ", m, err)
					}
				}
				for _, m := range h.LabelsRemoved {
					if err := g.delLabels(m.Message.Id, m.LabelIds); err != nil {
						log.Println("Delete label: ", m, err)
					}
				}
			}
			wg.Done()
		}()
	}
	i := uint(0)
	t := uint(0)
	for true {
		r, err := hist.PageToken(page).Do()
		if page == "" && err == errors.New("404") && historyId > 0 {
			// Full sync required.
			return fullSyncRequired
		} else if err != nil {
			return err
		}
		page = r.NextPageToken
		t += uint(len(r.History))
		for _, m := range r.History {
			if m.Id > historyId {
				historyId = m.Id
			}
			ms <- m
			g.progress <- lib.Progress{Current: i, Total: t}
			i++
		}
		if page == "" {
			break
		}
	}
	g.cache.SetHistoryIdx(historyId)
	return nil
}

func (g *Gmail) full() error {
	log.Println("Performing full sync.")
	getMsgs := g.svc.Messages.List("me")
	if g.labelId != "" {
		getMsgs.LabelIds(g.labelId)
	}
	page := ""
	ms := make(chan string, messageBufferSize)
	wg := sync.WaitGroup{}
	seen := make(map[string]struct{}) // Used to compute deletes.
	historyIds := make([]uint64, concurrentDownloads)
	for i := 0; i < concurrentDownloads; i++ {
		wg.Add(1)
		idx := i
		go func() {
			hist, err := g.processFullMessageList(ms)
			historyIds[idx] = hist
			if err != nil {
				log.Println(err)
			}
			wg.Done()
		}()
	}
	i := uint(0)
	t := uint(0)
	for true {
		r, err := getMsgs.PageToken(page).Do()
		if err != nil {
			return err
		}
		page = r.NextPageToken
		t += uint(r.ResultSizeEstimate)
		for _, m := range r.Messages {
			ms <- m.Id
			seen[m.Id] = struct{}{}
			g.progress <- lib.Progress{Current: i, Total: t}
			i++
		}
		if page == "" {
			break
		}
	}
	close(ms)
	wg.Wait()
	historyId := uint64(0)
	for _, i := range historyIds {
		if i > historyId {
			historyId = i
		}
	}
	if err := g.processDeletes(seen); err != nil {
		return err
	}
	g.cache.SetHistoryIdx(historyId)
	return nil
}

func (g *Gmail) Sync(full bool, progress chan<- lib.Progress) error {
	g.progress = progress
	g.limiter.Start()
	if g.label != "" {
		if l, err := g.labelToId(g.label); err != nil {
			return err
		} else {
			g.labelId = l
		}
	}
	// Get the cached history index.
	if hidx := g.cache.GetHistoryIdx(); hidx > 0 && !full {
		if err := g.incremental(hidx); err != nil {
			if err == fullSyncRequired {
				log.Println("History token expired--falling back to full sync")
				return g.full()
			}
		}
		return nil
	}
	return g.full()
}
