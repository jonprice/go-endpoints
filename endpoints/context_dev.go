// This implementation of Context interface uses tokeninfo API to validate
// bearer token.
//
// It is intended to be used only on dev server.

package endpoints

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/net/context"

	"google.golang.org/appengine"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/user"
)

const tokeninfoEndpointURL = "https://www.googleapis.com/oauth2/v2/tokeninfo"

type tokeninfo struct {
	IssuedTo      string `json:"issued_to"`
	Audience      string `json:"audience"`
	UserID        string `json:"user_id"`
	Scope         string `json:"scope"`
	ExpiresIn     int    `json:"expires_in"`
	Email         string `json:"email"`
	VerifiedEmail bool   `json:"verified_email"`
	AccessType    string `json:"access_type"`
	// ErrorDescription is populated when an error occurs. Usually, the response
	// either contains only ErrorDescription or the fields above
	ErrorDescription string `json:"error_description"`
}

// fetchTokeninfo retrieves token info from tokeninfoEndpointURL  (tokeninfo API)
func fetchTokeninfo(c Context, token string) (*tokeninfo, error) {
	url := tokeninfoEndpointURL + "?access_token=" + token
	log.Debugf(c, "Fetching token info from %q", url)
	resp, err := newHTTPClient(c).Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	log.Debugf(c, "Tokeninfo replied with %s", resp.Status)

	ti := &tokeninfo{}
	if err = json.NewDecoder(resp.Body).Decode(ti); err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("Error fetching tokeninfo (status %d)", resp.StatusCode)
		if ti.ErrorDescription != "" {
			errMsg += ": " + ti.ErrorDescription
		}
		return nil, errors.New(errMsg)
	}

	switch {
	case ti.ExpiresIn <= 0:
		return nil, errors.New("Token is expired")
	case !ti.VerifiedEmail:
		return nil, fmt.Errorf("Unverified email %q", ti.Email)
	case ti.Email == "":
		return nil, fmt.Errorf("Invalid email address")
	}

	return ti, err
}

// getScopedTokeninfo validates fetched token by matching tokeinfo.Scope
// with scope arg.
func getScopedTokeninfo(c Context, scope string) (*tokeninfo, error) {
	token := getToken(c.HTTPRequest())
	if token == "" {
		return nil, errors.New("No token found")
	}
	ti, err := fetchTokeninfo(c, token)
	if err != nil {
		return nil, err
	}
	for _, s := range strings.Split(ti.Scope, " ") {
		if s == scope {
			return ti, nil
		}
	}
	return nil, fmt.Errorf("No scope matches: expected one of %q, got %q",
		ti.Scope, scope)
}

// A context that uses tokeninfo API to validate bearer token
type tokeninfoContext struct {
	context.Context
	h *http.Request
}

func (c *tokeninfoContext) HTTPRequest() *http.Request {
	return c.h
}

// Namespace returns a replacement context that operates within the given namespace.
func (c *tokeninfoContext) Namespace(name string) (Context, error) {
	nc, err := appengine.Namespace(c, name)
	if err != nil {
		return nil, err
	}
	return &tokeninfoContext{nc, c.h}, nil
}

// CurrentOAuthClientID returns a clientID associated with the scope.
func (c *tokeninfoContext) CurrentOAuthClientID(scope string) (string, error) {
	ti, err := getScopedTokeninfo(c, scope)
	if err != nil {
		return "", err
	}
	return ti.IssuedTo, nil
}

// CurrentOAuthUser returns a user associated with the request in context.
func (c *tokeninfoContext) CurrentOAuthUser(scope string) (*user.User, error) {
	ti, err := getScopedTokeninfo(c, scope)
	if err != nil {
		return nil, err
	}
	return &user.User{Email: ti.Email}, nil
}

// tokeninfoContextFactory creates a new tokeninfoContext from r.
// To be used as auth.go/ContextFactory.
func tokeninfoContextFactory(r *http.Request) Context {
	ac := appengine.NewContext(r)
	return &tokeninfoContext{ac, r}
}
