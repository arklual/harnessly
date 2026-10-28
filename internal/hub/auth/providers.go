package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/ly-solutions/harnessly/internal/hub/config"
)

// getJSON calls an API of a provider with the token and decodes the answer.
func (s *Service) getJSON(ctx context.Context, url string, header http.Header, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("Accept", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %d", req.URL.Host, res.StatusCode)
	}
	return json.Unmarshal(body, out)
}

// GitHub.

func (s *Service) githubProvider() *oauthProvider {
	base := s.cfg.GitHub.URL
	api := base + "/api/v3"
	if base == "https://github.com" {
		api = "https://api.github.com"
	}
	oc := &oauth2.Config{
		ClientID: s.cfg.GitHub.ClientID, ClientSecret: s.cfg.GitHub.ClientSecret,
		Endpoint:    oauth2.Endpoint{AuthURL: base + "/login/oauth/authorize", TokenURL: base + "/login/oauth/access_token", AuthStyle: oauth2.AuthStyleInParams},
		RedirectURL: s.callbackURL("github"),
		Scopes:      []string{"read:user", "user:email"},
	}
	return &oauthProvider{
		route:  "github",
		config: func(context.Context) (*oauth2.Config, error) { return oc, nil },
		identity: func(ctx context.Context, _ *oauth2.Config, tok *oauth2.Token, _ string) (identityInfo, error) {
			h := http.Header{"Authorization": {"Bearer " + tok.AccessToken}, "X-GitHub-Api-Version": {"2022-11-28"}}
			var user struct {
				ID    int64  `json:"id"`
				Login string `json:"login"`
				Name  string `json:"name"`
			}
			if err := s.getJSON(ctx, api+"/user", h, &user); err != nil {
				return identityInfo{}, err
			}
			var emails []struct {
				Email    string `json:"email"`
				Primary  bool   `json:"primary"`
				Verified bool   `json:"verified"`
			}
			if err := s.getJSON(ctx, api+"/user/emails", h, &emails); err != nil {
				return identityInfo{}, err
			}
			// Only the primary, verified address counts: GitHub lets an account list addresses it has not proved.
			for _, e := range emails {
				if e.Primary && e.Verified {
					name := user.Name
					if name == "" {
						name = user.Login
					}
					return identityInfo{Provider: "github", Subject: strconv.FormatInt(user.ID, 10), Email: e.Email, Name: name, EmailVerified: true}, nil
				}
			}
			return identityInfo{}, errNoEmail
		},
	}
}

// Yandex ID.

func (s *Service) yandexProvider() *oauthProvider {
	ep := s.ep
	if ep.YandexAuth == "" {
		ep.YandexAuth = "https://oauth.yandex.ru/authorize"
	}
	if ep.YandexToken == "" {
		ep.YandexToken = "https://oauth.yandex.ru/token"
	}
	if ep.YandexInfo == "" {
		ep.YandexInfo = "https://login.yandex.ru/info?format=json"
	}
	oc := &oauth2.Config{
		ClientID: s.cfg.Yandex.ClientID, ClientSecret: s.cfg.Yandex.ClientSecret,
		Endpoint:    oauth2.Endpoint{AuthURL: ep.YandexAuth, TokenURL: ep.YandexToken, AuthStyle: oauth2.AuthStyleInParams},
		RedirectURL: s.callbackURL("yandex"),
		Scopes:      []string{"login:email", "login:info"},
	}
	return &oauthProvider{
		route:  "yandex",
		config: func(context.Context) (*oauth2.Config, error) { return oc, nil },
		identity: func(ctx context.Context, _ *oauth2.Config, tok *oauth2.Token, _ string) (identityInfo, error) {
			var info struct {
				ID           string `json:"id"`
				DefaultEmail string `json:"default_email"`
				RealName     string `json:"real_name"`
				DisplayName  string `json:"display_name"`
				Login        string `json:"login"`
			}
			if err := s.getJSON(ctx, ep.YandexInfo, http.Header{"Authorization": {"OAuth " + tok.AccessToken}}, &info); err != nil {
				return identityInfo{}, err
			}
			if info.ID == "" || info.DefaultEmail == "" {
				return identityInfo{}, errNoEmail
			}
			name := info.RealName
			if name == "" {
				name = info.DisplayName
			}
			if name == "" {
				name = info.Login
			}
			// Yandex ID does not say whether the address is confirmed, so it is never trusted to join an account.
			return identityInfo{Provider: "yandex", Subject: info.ID, Email: info.DefaultEmail, Name: name, EmailVerified: false}, nil
		},
	}
}

// OpenID Connect.

// flexBool reads a JSON boolean that some providers send as the string "true".
type flexBool bool

func (b *flexBool) UnmarshalJSON(data []byte) error {
	switch strings.Trim(string(data), `"`) {
	case "true":
		*b = true
	default:
		*b = false
	}
	return nil
}

func (s *Service) oidcProvider(id string) *oauthProvider {
	var conf config.OIDC
	for _, p := range s.cfg.OIDC {
		if p.ID == id {
			conf = p
		}
	}
	route := "oidc-" + id
	var (
		mu       sync.Mutex
		provider *oidc.Provider
		oc       *oauth2.Config
	)
	// discover finds the provider's endpoints on first use and keeps them; a provider that is down at start does not
	// keep the hub from starting.
	discover := func(ctx context.Context) (*oidc.Provider, *oauth2.Config, error) {
		mu.Lock()
		defer mu.Unlock()
		if provider != nil {
			return provider, oc, nil
		}
		p, err := oidc.NewProvider(ctx, conf.Issuer)
		if err != nil {
			return nil, nil, err
		}
		scopes := conf.Scopes
		if len(scopes) == 0 {
			scopes = []string{oidc.ScopeOpenID, "email", "profile"}
		}
		provider = p
		oc = &oauth2.Config{ClientID: conf.ClientID, ClientSecret: conf.ClientSecret, Endpoint: p.Endpoint(), RedirectURL: s.callbackURL(route), Scopes: scopes}
		return provider, oc, nil
	}
	return &oauthProvider{
		route: route,
		config: func(ctx context.Context) (*oauth2.Config, error) {
			_, c, err := discover(oidc.ClientContext(ctx, s.client))
			return c, err
		},
		authOptions: func(nonce string) []oauth2.AuthCodeOption { return []oauth2.AuthCodeOption{oidc.Nonce(nonce)} },
		identity: func(ctx context.Context, _ *oauth2.Config, tok *oauth2.Token, nonce string) (identityInfo, error) {
			ctx = oidc.ClientContext(ctx, s.client)
			p, _, err := discover(ctx)
			if err != nil {
				return identityInfo{}, err
			}
			raw, _ := tok.Extra("id_token").(string)
			if raw == "" {
				return identityInfo{}, errors.New("no id_token in the answer")
			}
			idt, err := p.Verifier(&oidc.Config{ClientID: conf.ClientID}).Verify(ctx, raw)
			if err != nil {
				return identityInfo{}, err
			}
			if idt.Nonce != nonce || nonce == "" {
				return identityInfo{}, errors.New("nonce does not match")
			}
			var claims struct {
				Email             string   `json:"email"`
				EmailVerified     flexBool `json:"email_verified"`
				Name              string   `json:"name"`
				PreferredUsername string   `json:"preferred_username"`
			}
			if err := idt.Claims(&claims); err != nil {
				return identityInfo{}, err
			}
			if claims.Email == "" {
				// Some providers keep the address out of the token and give it at the userinfo endpoint.
				if ui, err := p.UserInfo(ctx, oauth2.StaticTokenSource(tok)); err == nil && ui.Subject == idt.Subject {
					var more struct {
						Email         string   `json:"email"`
						EmailVerified flexBool `json:"email_verified"`
						Name          string   `json:"name"`
					}
					if ui.Claims(&more) == nil {
						claims.Email, claims.EmailVerified = more.Email, more.EmailVerified
						if claims.Name == "" {
							claims.Name = more.Name
						}
					}
				}
			}
			if claims.Name == "" {
				claims.Name = claims.PreferredUsername
			}
			if claims.Email == "" {
				return identityInfo{}, errNoEmail
			}
			return identityInfo{Provider: "oidc:" + id, Subject: idt.Subject, Email: claims.Email, Name: claims.Name, EmailVerified: bool(claims.EmailVerified)}, nil
		},
	}
}
