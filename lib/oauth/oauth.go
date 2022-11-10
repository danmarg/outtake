// Package oauth implements a convenience function for doing the Oauth exchange.
package oauth

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"time"

	"golang.org/x/net/context"
	"golang.org/x/oauth2"
)

const (
	// Oauth client ID.
	ClientId = "457311175792-n3hpckfadgri6opat70c8an1fmhmaev7.apps.googleusercontent.com"
	// Oauth client secret.
	Secret = "GOylH6-BUUQFm_lzrhXKpdac"
)

func GetOAuthClient(ctx context.Context, cfg *oauth2.Config) (*oauth2.Token, error) {
	tok := new(oauth2.Token)
	// Have to get a new token.
	print("Launching browser for OAuth exchange. To skip, rerun with environment variable 'OAUTH' set to 'NOBROWSER'.\n")
	code, err := tokenFromWeb(ctx, cfg)
	if err == nil {
		tok, err = cfg.Exchange(ctx, code)
	}
	return tok, err
}

func tokenFromWeb(ctx context.Context, config *oauth2.Config) (string, error) {
	ch := make(chan string)
	randState := fmt.Sprintf("st%d", time.Now().UnixNano())
	ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/favicon.ico" {
			http.Error(rw, "", 404)
			return
		}
		if req.FormValue("state") != randState {
			log.Printf("State doesn't match: req = %#v", req)
			http.Error(rw, "", 500)
			return
		}
		if code := req.FormValue("code"); code != "" {
			fmt.Fprintf(rw, "<h1>Success</h1>Authorized.")
			rw.(http.Flusher).Flush()
			ch <- code
			return
		}
		http.Error(rw, "", 500)
	}))
	defer ts.Close()
	config.RedirectURL = ts.URL
	authURL := config.AuthCodeURL(randState)
	errs := make(chan error)
	go func() {
		err := openURL(authURL)
		errs <- err
	}()
	err := <-errs
	if err == nil {
		code := <-ch
		return code, nil
	} else {
		return "", err
	}
}

func openURL(url string) error {
	try := []string{"xdg-open", "google-chrome", "open"}
	for _, bin := range try {
		err := exec.Command(bin, url).Run()
		if err == nil {
			return nil
		}
	}
	fmt.Printf("Open %v in your browser.", url)
	return nil
}
