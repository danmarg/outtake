// Package maildir implements reading and writing maildir directories as specified in http://cr.yp.to/proto/maildir.html.
package maildir

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/mail"
	"os"
	"path"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	cur = "cur"
	tmp = "tmp"
	nw  = "new"
)

var (
	pid      int
	cntr     uint64
	hostname string
)

func init() {
	pid = os.Getpid()
	h, _ := os.Hostname()
	hostname = strings.Replace(strings.Replace(h, "/", "\057", -1), ":", "\072", -1)
}

// Key is a key of a maildir message.
type Key string

// Maildir is a single maildir directory.
type Maildir struct {
	dir string
}

// Create creates a maildir rooted at dir.
func Create(dir string) (Maildir, error) {
	m := Maildir{dir}
	for _, x := range []string{cur, tmp, nw} {
		if err := os.MkdirAll(path.Join(dir, x), 0766); err != nil {
			return m, err
		}
	}
	return m, nil
}

// Deliver delivers the Message to the "new" maildir.
func (d Maildir) Deliver(m *mail.Message) (Key, error) {
	k := strconv.FormatInt(time.Now().Unix(), 10) + "."
	k += strconv.FormatInt(int64(pid), 10) + "_" + strconv.FormatUint(atomic.AddUint64(&cntr, 1), 10)
	k += "." + hostname
	key := Key(k)
	f, err := os.Create(path.Join(d.dir, tmp, k))
	if err != nil {
		return key, err
	}
	defer f.Close()
	for h, vs := range m.Header {
		for _, v := range vs {
			if _, err := f.WriteString(h + ": " + v + "\n"); err != nil {
				return key, err
			}
		}
	}
	if _, err := f.WriteString("\r\n"); err != nil {
		return key, err
	}
	if _, err := io.Copy(f, m.Body); err != nil {
		return key, err
	}
	return key, os.Rename(path.Join(d.dir, tmp, k), path.Join(d.dir, nw, k))
}

// GetFile gets the file path for the specified key.
func (d Maildir) GetFile(k Key) (string, error) {
	// Check in new.
	f := path.Join(d.dir, nw, string(k))
	if _, err := os.Stat(f); err == nil {
		return f, nil
	}
	// Check in cur.
	fs, err := ioutil.ReadDir(path.Join(d.dir, cur))
	if err != nil {
		return "", err
	}
	for _, f := range fs {
		if strings.HasPrefix(f.Name(), string(k)+":") {
			return path.Join(d.dir, cur, f.Name()), nil
		}
	}
	return "", fmt.Errorf("Does not exist")
}

// Delete removes the message with the specified key from cur/new.
func (d Maildir) Delete(k Key) error {
	f, err := d.GetFile(k)
	if err != nil {
		return err
	}
	return os.Remove(f)
}
