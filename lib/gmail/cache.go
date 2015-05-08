package gmail

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"

	"github.com/danmarg/outtake/lib"
	"github.com/danmarg/outtake/lib/maildir"
	"golang.org/x/oauth2"
)

const (
	// Cache key prefixes.
	midToKey     = "mid_to_key"
	midToLabels  = "mid_to_label"
	historyIndex = "history_index"
	oauthToken   = "oauth_token"
)

type gmailCache struct {
	Cache lib.Cache
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
