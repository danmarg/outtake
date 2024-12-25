package gmail

import (
	"strings"
	"time"

	"github.com/danmarg/outtake/lib"
	gmail "google.golang.org/api/gmail/v1"
	"google.golang.org/api/googleapi"
)

const (
	maxQps     = 50
	maxRetries = 8
)

// Wrapper for the Gmail REST interface. This abstraction helps with unit testing.
type gmailService interface {
	GetRawMessage(id string) (string, error)
	GetMetadata(id string) (*gmail.Message, error)
	GetLabels() (*gmail.ListLabelsResponse, error)
	GetHistory(historyIndex uint64, label, page string) (*gmail.ListHistoryResponse, error)
	GetMessages(q, page string) (*gmail.ListMessagesResponse, error)
}

type backoff struct {
	count uint
}

type restGmailService struct {
	gmailService
	svc     *gmail.UsersService
	limiter lib.RateLimit
}

func newRestGmailService(svc *gmail.UsersService) *restGmailService {
	r := &restGmailService{svc: svc,
		limiter: lib.RateLimit{Period: time.Second,
			Rate:         maxQps,
			BackoffLimit: maxRetries,
			BackoffStart: time.Second}}
	r.limiter.Start()
	return r
}

func isRateLimited(err error) (error, bool) {
	e, ok := err.(*googleapi.Error)
	return err, !(ok && (e.Code == 429 ||
		// See https://developers.google.com/gmail/api/guides/handle-errors
		(e.Code == 403 && strings.Contains(e.Message, "Rate Limit"))))
}

func (s *restGmailService) GetRawMessage(id string) (string, error) {
	var r *gmail.Message
	var err error
	err = s.limiter.DoWithBackoff(func() (error, bool) {
		r, err = s.svc.Messages.Get("me", id).Format("raw").Do()
		return isRateLimited(err)
	})
	if r != nil {
		return r.Raw, err
	}
	return "", err
}

func (s *restGmailService) GetMetadata(id string) (*gmail.Message, error) {
	var m *gmail.Message
	var err error
	err = s.limiter.DoWithBackoff(func() (error, bool) {
		m, err = s.svc.Messages.Get("me", id).Format("metadata").Do()
		return isRateLimited(err)
	})
	return m, err
}

func (s *restGmailService) GetLabels() (*gmail.ListLabelsResponse, error) {
	var r *gmail.ListLabelsResponse
	var err error
	err = s.limiter.DoWithBackoff(func() (error, bool) {
		r, err = s.svc.Labels.List("me").Do()
		return isRateLimited(err)
	})
	return r, err
}

func (s *restGmailService) GetHistory(historyIndex uint64, labelId, page string) (*gmail.ListHistoryResponse, error) {
	hist := s.svc.History.List("me").StartHistoryId(historyIndex)
	if labelId != "" {
		hist.LabelId(labelId)
	}
	var r *gmail.ListHistoryResponse
	var err error
	err = s.limiter.DoWithBackoff(func() (error, bool) {
		r, err = hist.PageToken(page).Do()
		return isRateLimited(err)
	})
	return r, err
}

func (s *restGmailService) GetMessages(labelId, page string) (*gmail.ListMessagesResponse, error) {
	// XXX: -in:chats to skip non-email results that the API returns.
	msgs := s.svc.Messages.List("me").Q("-in:chats")
	if labelId != "" {
		msgs.LabelIds(labelId)
	}
	var r *gmail.ListMessagesResponse
	var err error
	err = s.limiter.DoWithBackoff(func() (error, bool) {
		r, err = msgs.PageToken(page).Do()
		return isRateLimited(err)
	})
	return r, err
}
