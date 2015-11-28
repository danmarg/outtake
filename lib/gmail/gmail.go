// Package gmail implements the guts of the Gmail sync logic. Gmail
// synchronization can happen in one o two ways: full sync, and incremental.
// Incremental is possible when we have an existing "history index", a value
// that tells the Gmail API where we last synced to; in this case, the API
// tells us exactly what messages have been added, deleted, or moved (i.e.
// labels changed) since the last sync. In comparison, in a full sync, we must
// retrieve all messages present on the server and their labels in order to
// compute label changes, and deduce message deletions by comparing messages we
// know about with those present on the server.
//
// To abstract this a bit, and to parallelize slower network operations, our
// flow looks like this:
//     full() --> getBody() --> getMetaData() --> writeAdd()
//            --> getMetaData() --> writeLabels()
//            --> writeDel()
//
//     incremental() --> getBody() --> getMetaData() --> writeAdd()
//                   --> writeLabels()
//                   --> writeDel()
// getBody() and getMetaData() make RPCs to the Gmail API, and multiple
// workers run in parallel.

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
	"strconv"
	"sync"

	"github.com/danmarg/outtake/lib"
	"github.com/danmarg/outtake/lib/maildir"
	"github.com/danmarg/outtake/lib/oauth"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	gmail "google.golang.org/api/gmail/v1"
	"google.golang.org/api/googleapi"
)

const (
	// What X- header to use for storing labels.
	labelsHeader = "X-Keywords"
	// Cache filename.
	cacheFile = ".outtake"
)

var (
	// Errors.
	unknownMessage   = errors.New("unknown message")
	fullSyncRequired = errors.New("full sync required")
	// Parallelism.
	MessageBufferSize   = 128
	ConcurrentDownloads = 8
)

// Gmail represents a Gmail client.
type Gmail struct {
	label    string
	labelId  string
	cache    gmailCache
	svc      gmailService
	dir      maildir.Maildir
	progress chan<- lib.Progress
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
		// XXX: should we use a client-specified context here?
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
		g.svc = newRestGmailService(gmail.NewUsersService(c))
	}
	if d, err := maildir.Create(dir); err != nil {
		return nil, err
	} else {
		g.dir = d
	}

	return &g, nil
}

const (
	NONE         = iota
	ADD          = iota
	DELETE       = iota
	WRITE_LABELS = iota
)

type msgOp struct {
	Id        string
	HistoryId uint64
	Labels    []string
	Msg       *mail.Message
	Operation int32
	Error     error
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

func (g *Gmail) getBody(m string) (*mail.Message, error) {
	body, err := g.svc.GetRawMessage(m)
	if err != nil {
		return nil, err
	}
	raw, err := base64.URLEncoding.DecodeString(body)
	if err != nil {
		return nil, err
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		log.Println("Error parsing message", m, ":", err)
		// XXX: Don't return an error here. These are often chats and such, due to bugs in the Gmail API.
		return nil, nil
	}
	return msg, nil
}

func (g *Gmail) getMetaData(m *msgOp) error {
	meta, err := g.svc.GetMetadata(m.Id)
	if err != nil {
		return err
	}
	m.Labels = meta.LabelIds
	m.HistoryId = meta.HistoryId
	return err
}

func (g *Gmail) writeAdd(m msgOp) error {
	k, err := g.dir.Deliver(m.Msg)
	if err != nil {
		return err
	}
	// Update the cache.
	g.cache.SetMsgLabels(m.Id, m.Labels)
	g.cache.SetMsgKey(m.Id, k)
	return nil
}

func (g *Gmail) writeDel(id string) error {
	k, ok := g.cache.GetMsgKey(id)
	if !ok {
		// XXX: It doesn't make sense to error out here, since we're deleting anyway...
		return nil
	}
	if err := g.dir.Delete(k); err != nil {
		return err
	}
	g.cache.DelMsg(id)
	return nil
}

func (g *Gmail) computeLabels(id string, added, removed []string) []string {
	if old, ok := g.cache.GetMsgLabels(id); ok {
		nlabels := make(map[string]struct{})
		for _, l := range old {
			nlabels[l] = struct{}{}
		}
		for _, l := range added {
			nlabels[l] = struct{}{}
		}
		for _, l := range removed {
			delete(nlabels, l)
		}
		labels := make([]string, len(nlabels))
		i := 0
		for l, _ := range nlabels {
			labels[i] = l
			i++
		}
		return labels
	}
	// This shouldn't happen--there should always be a cache hit--but OK.
	return added
}

func (g *Gmail) labelsChanged(id string, newLabels []string) bool {
	if old, ok := g.cache.GetMsgLabels(id); ok {
		sort.Strings(old)
		sort.Strings(newLabels)
		if old != nil && len(old) == len(newLabels) {
			for i := 0; i < len(old); i++ {
				if old[i] != newLabels[i] {
					return true
				}
			}
			return false
		} else {
			return true
		}
	}
	return true
}

func (g *Gmail) writeLabels(id string, labels []string) error {
	k, ok := g.cache.GetMsgKey(id)
	if !ok {
		log.Println("unknown message", id, "for write labels")
		// XXX: Seems the API gives us label changes for messages we've never seen before that don't current exist. Dunno why.
		return nil //unknownMessage
	}
	msg, c, err := g.getMaildirMessage(k)
	if err != nil {
		return err
	}
	defer c.Close()
	msg.Header[labelsHeader] = labels
	// Note that this will mark a message as "new" for any clients. This might be undesirable if only labels have changed?
	kn, err := g.dir.Deliver(msg)
	if err != nil {
		return err
	}
	// Update the cache.
	g.cache.SetMsgLabels(id, labels)
	g.cache.SetMsgKey(id, kn)
	// Delete the old message
	if err := g.dir.Delete(k); err != nil {
		return err
	}
	return nil
}

func (g *Gmail) labelToId(label string) (string, error) {
	ls, err := g.svc.GetLabels()
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

func (g *Gmail) handleNewMsg(id string) msgOp {
	k, exists := g.cache.GetMsgKey(id)
	o := msgOp{Id: id}
	if !exists {
		o.Operation = ADD
		m, err := g.getBody(id)
		if err != nil || m == nil {
			if e, ok := err.(*googleapi.Error); ok && e.Code == 404 {
				// XXX: 404 on a message add probably means it was deleted later. OK.
			} else {
				o.Error = err
			}
			o.Operation = NONE
			return o
		}
		o.Msg = m
	}
	if err := g.getMetaData(&o); err != nil {
		o.Error = err
		return o
	}
	if g.labelsChanged(id, o.Labels) && exists {
		// Have to fetch body.
		m, c, err := g.getMaildirMessage(k)
		if err != nil {
			o.Error = err
			return o
		}
		defer c.Close()
		o.Msg = m
		o.Operation = WRITE_LABELS
		o.Msg.Header[labelsHeader] = o.Labels
	} else if o.Operation == ADD {
		o.Msg.Header[labelsHeader] = o.Labels
	}
	return o
}

func shardForMsgId(id string) int {
	shard, _ := strconv.ParseUint(id, 16, 64)
	shard = shard % uint64(ConcurrentDownloads)
	return int(shard)
}

func (g *Gmail) incremental(historyId uint64) error {
	log.Println("Performing incremental sync.")
	page := ""
	// histEvents is an array of channels, where each channel receives a shard of
	// history events. We can thus guarantee that all history events for a single
	// message ID are handled by the same shard, and thus their resulting
	// mailbox operations will be enqueued into "ops" in order.
	histEvents := make([]chan msgOp, ConcurrentDownloads)
	for i := 0; i < len(histEvents); i++ {
		histEvents[i] = make(chan msgOp, MessageBufferSize)
	}
	ops := make(chan msgOp, MessageBufferSize)

	// Process new messages. This spins off ConcurrentDownloads goroutines that
	// download message bodies and labels.
	// Because a sequence of history events might look like:
	//    1. add message 0x123
	//    2. change labels message 0x123
	// it's important that all events for message 0x123 be processed sequentially.
	// We do that by sharding history events by message ID, so that the same
	// goroutine always gets the same messages. So to do that, we have to have
	// "ConcurrentDownloads" channels, one for each goroutine.
	wg := sync.WaitGroup{}
	for i := 0; i < ConcurrentDownloads; i++ {
		idx := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			for op := range histEvents[idx] {
				if op.Operation == ADD {
					ops <- g.handleNewMsg(op.Id)
				} else {
					ops <- op
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(ops)
	}()

	t := uint(0) // Total count, for progress reporting.
	go func() {
		for true {
			r, err := g.svc.GetHistory(historyId, g.labelId, page)
			if e, ok := err.(*googleapi.Error); ok && e.Code == 404 && page == "" && historyId > 0 {
				// Full sync required.
				ops <- msgOp{Error: fullSyncRequired}
				return
			} else if err != nil {
				ops <- msgOp{Error: err}
				return
			}
			page = r.NextPageToken
			t += uint(len(r.History))
			for _, m := range r.History {
				if m.Id > historyId {
					historyId = m.Id
				}
				// Enqueue adds.
				for _, a := range m.MessagesAdded {
					shard := shardForMsgId(a.Message.Id)
					histEvents[shard] <- msgOp{Id: a.Message.Id, Operation: ADD, HistoryId: m.Id}
				}
				// Enqueue deletes.
				for _, d := range m.MessagesDeleted {
					shard := shardForMsgId(d.Message.Id)
					histEvents[shard] <- msgOp{Id: d.Message.Id, Operation: DELETE, HistoryId: m.Id}
				}
				// Enqueue label changes. First we have to compute what the real labels are.
				type lchange struct {
					Added   []string
					Removed []string
				}
				labels := make(map[string]lchange)
				for _, l := range m.LabelsAdded {
					if ls, ok := labels[l.Message.Id]; ok {
						labels[l.Message.Id] = lchange{
							Added:   append(ls.Added, l.LabelIds...),
							Removed: ls.Removed}
					} else {
						labels[l.Message.Id] = lchange{Added: l.LabelIds, Removed: []string{}}
					}
				}
				for _, l := range m.LabelsRemoved {
					if ls, ok := labels[l.Message.Id]; ok {
						labels[l.Message.Id] = lchange{
							Added:   ls.Added,
							Removed: append(ls.Removed, l.LabelIds...)}
					} else {
						labels[l.Message.Id] = lchange{Removed: l.LabelIds, Added: []string{}}
					}
				}
				for id, changes := range labels {
					newLabels := g.computeLabels(id, changes.Added, changes.Removed)
					if g.labelsChanged(id, newLabels) {
						shard := shardForMsgId(id)
						histEvents[shard] <- msgOp{Id: id, Labels: newLabels, Operation: WRITE_LABELS, HistoryId: m.Id}
					}
				}
			}
			if page == "" {
				break
			}
		}
		for _, h := range histEvents {
			close(h)
		}
	}()
	i := uint(0)
	for o := range ops {
		// Update progress bar.
		if g.progress != nil {
			g.progress <- lib.Progress{Current: i, Total: t}
		}
		i++
		if o.Error != nil {
			return o.Error
		}
		if o.Operation == NONE {
			continue
		}
		if err := g.writeOperation(o); err != nil {
			return err
		}
	}
	g.cache.SetHistoryIdx(historyId)
	return nil
}

func (g *Gmail) writeOperation(o msgOp) error {
	switch o.Operation {
	case ADD:
		if err := g.writeAdd(o); err != nil {
			return err
		}
	case DELETE:
		if err := g.writeDel(o.Id); err != nil {
			return err
		}
	case WRITE_LABELS:
		if err := g.writeLabels(o.Id, o.Labels); err != nil {
			return err
		}
	}
	return nil
}

func (g *Gmail) full() error {
	log.Println("Performing full sync.")
	// XXX: -in:chats to skip chats that aren't MIME messages.
	newMsgs := make(chan string, MessageBufferSize)
	ops := make(chan msgOp, MessageBufferSize)
	wg := sync.WaitGroup{}
	for i := 0; i < ConcurrentDownloads; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range newMsgs {
				ops <- g.handleNewMsg(id)
			}
		}()
	}
	go func() {
		wg.Wait()
		close(ops)
	}()
	seen := make(map[string]struct{}) // Used to compute deletes.
	t := uint(0)                      // Total count, for progress reporting.
	go func() {
		defer close(newMsgs)
		page := ""
		for true {
			r, err := g.svc.GetMessages(g.labelId, page)
			if err != nil {
				ops <- msgOp{Error: err}
				return
			}
			page = r.NextPageToken
			t += uint(r.ResultSizeEstimate)
			for _, m := range r.Messages {
				newMsgs <- m.Id
				seen[m.Id] = struct{}{}
			}
			if page == "" {
				break
			}
		}
	}()
	historyId := uint64(0)
	i := uint(0) // For updating progress bar.
	for o := range ops {
		// Update progress bar.
		if g.progress != nil {
			g.progress <- lib.Progress{Current: i, Total: t}
		}
		i++
		if o.Error != nil {
			return o.Error
		}
		if o.Operation == NONE {
			continue
		}
		if o.HistoryId > historyId {
			historyId = o.HistoryId
		}
		if err := g.writeOperation(o); err != nil {
			return err
		}
	}
	is := make(chan string)
	g.cache.GetMsgs(is)
	for i := range is {
		if _, ok := seen[i]; !ok {
			if err := g.writeDel(i); err != nil {
				return err
			}
		}
	}
	g.cache.SetHistoryIdx(historyId)
	return nil
}

func (g *Gmail) Sync(full bool, progress chan<- lib.Progress) error {
	g.progress = progress
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
			return err
		}
		return nil
	}
	return g.full()
}
