package lib

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"time"

	"golang.org/x/net/context"
	"golang.org/x/oauth2"
)

func getOAuthClient(ctx context.Context, cfg *oauth2.Config) (*oauth2.Token, error) {
	tok := new(oauth2.Token)
	// Have to get a new token.
	browser := os.Getenv("OAUTH") != "NOBROWSER"
	code := ""
	var err error
	if browser {
		print("Launching browser for OAuth exchange. To skip, rerun with environment variable 'OAUTH' set to 'NOBROWSER'.\n")
		code, err = tokenFromWeb(ctx, cfg)
	}
	if err != nil || !browser {
		// Fall back to non-browser auth by rewriting the redirect URL and reading the auth code from stdin.
		cfg.RedirectURL = "urn:ietf:wg:oauth:2.0:oob"
		authURL := cfg.AuthCodeURL("")
		fmt.Printf("Authorize this app at %s and paste the authorization code.\n> ", authURL)
		_, err = fmt.Scanf("%s", &code)
	}
	tok, err = cfg.Exchange(ctx, code)
	return tok, nil
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
	return fmt.Errorf("Error opening URL in browser.")
}
