package tbotjoin

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// ManagedBy is the label the hub puts on the tokens it creates (app.kubernetes.io/managed-by). A token without it was
// made by somebody else (a manifest in git): the hub neither replaces nor deletes it.
const (
	ManagedByLabel = "app.kubernetes.io/managed-by"
	ManagedBy      = "hub"
)

// OwnerTrait is the trait of a bot that names the person it belongs to, by email.
const OwnerTrait = "owner"

// Recovery is how often a bot whose token the hub made may join again after it lost its state.
const recoveryLimit = 20

var (
	// ErrNoBot: the cluster has no bot of that name.
	ErrNoBot = errors.New("no such bot")
	// ErrNotOurs: a token of that name exists and the hub did not make it.
	ErrNotOurs = errors.New("the token was not made by the hub")
)

// Cluster is where the join tokens live: the resources of the Teleport operator.
type Cluster interface {
	// BotOwners are the emails the bot's owner trait names; ErrNoBot when there is no such bot.
	BotOwners(ctx context.Context, bot string) ([]string, error)
	// PutToken makes the token bound to the key, replacing one the hub made before; ErrNotOurs when one of that name
	// was made by somebody else.
	PutToken(ctx context.Context, name, bot, publicKey string) error
	// DeleteToken takes away a token the hub made; one that is gone already is fine, one somebody else made is
	// ErrNotOurs.
	DeleteToken(ctx context.Context, name string) error
}

// Kube is the Cluster through the Kubernetes API, with the service account's token the hub's pod carries.
type Kube struct {
	// URL is the API server; Namespace is the one the Teleport operator watches.
	URL, Namespace string
	// TokenFile is read on every request (a projected token is renewed in place); CAFile is the API server's CA.
	TokenFile, CAFile string
	// Client is for tests; nil: one that trusts CAFile.
	Client *http.Client
}

const (
	tokensPath = "/apis/resources.teleport.dev/v2/namespaces/%s/teleportprovisiontokens"
	botsPath   = "/apis/resources.teleport.dev/v1/namespaces/%s/teleportbotsv1/%s"
)

func (k *Kube) client() (*http.Client, error) {
	if k.Client != nil {
		return k.Client, nil
	}
	pem, err := os.ReadFile(k.CAFile)
	if err != nil {
		return nil, fmt.Errorf("the cluster's CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("the cluster's CA holds no certificate")
	}
	k.Client = &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}
	return k.Client, nil
}

// do sends one request; a status out of 2xx is an *apiError.
func (k *Kube) do(ctx context.Context, method, path string, body any, out any) error {
	c, err := k.client()
	if err != nil {
		return err
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, k.URL+path, rd)
	if err != nil {
		return err
	}
	if k.TokenFile != "" {
		tok, err := os.ReadFile(k.TokenFile)
		if err != nil {
			return fmt.Errorf("the service account's token: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(tok)))
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode/100 != 2 {
		e := &apiError{Status: res.StatusCode}
		var st struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(data, &st) == nil {
			e.Message = st.Message
		}
		return e
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string { return fmt.Sprintf("kubernetes API %d: %s", e.Status, e.Message) }

func statusOf(err error) int {
	var e *apiError
	if errors.As(err, &e) {
		return e.Status
	}
	return 0
}

// BotOwners reads the bot's owner trait.
func (k *Kube) BotOwners(ctx context.Context, bot string) ([]string, error) {
	var b struct {
		Spec struct {
			Traits []struct {
				Name   string   `json:"name"`
				Values []string `json:"values"`
			} `json:"traits"`
		} `json:"spec"`
	}
	err := k.do(ctx, http.MethodGet, fmt.Sprintf(botsPath, url.PathEscape(k.Namespace), url.PathEscape(bot)), nil, &b)
	if statusOf(err) == http.StatusNotFound {
		return nil, ErrNoBot
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, t := range b.Spec.Traits {
		if t.Name == OwnerTrait {
			out = append(out, t.Values...)
		}
	}
	return out, nil
}

type objectMeta struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	ResourceVersion   string            `json:"resourceVersion,omitempty"`
	DeletionTimestamp *string           `json:"deletionTimestamp,omitempty"`
}

// token reads a token's metadata; found false when there is none.
func (k *Kube) token(ctx context.Context, name string) (objectMeta, bool, error) {
	var t struct {
		Metadata objectMeta `json:"metadata"`
	}
	err := k.do(ctx, http.MethodGet, fmt.Sprintf(tokensPath, url.PathEscape(k.Namespace))+"/"+url.PathEscape(name), nil, &t)
	if statusOf(err) == http.StatusNotFound {
		return objectMeta{}, false, nil
	}
	if err != nil {
		return objectMeta{}, false, err
	}
	return t.Metadata, true, nil
}

// TokenObject is the TeleportProvisionToken the hub creates: the Bot role, the bound_keypair method with the machine's
// public key, standard recovery. Nothing else: no other role, no other join method.
func TokenObject(namespace, name, bot, publicKey string) map[string]any {
	return map[string]any{
		"apiVersion": "resources.teleport.dev/v2",
		"kind":       "TeleportProvisionToken",
		"metadata": objectMeta{Name: name, Namespace: namespace, Labels: map[string]string{
			ManagedByLabel: ManagedBy,
		}},
		"spec": map[string]any{
			"roles":       []string{"Bot"},
			"bot_name":    bot,
			"join_method": "bound_keypair",
			"bound_keypair": map[string]any{
				"onboarding": map[string]any{"initial_public_key": publicKey},
				"recovery":   map[string]any{"mode": "standard", "limit": recoveryLimit},
			},
		},
	}
}

// PutToken makes the token. One the hub made before is deleted first and waited for (up to 20 s): the key of a bound
// token is not changed in place.
func (k *Kube) PutToken(ctx context.Context, name, bot, publicKey string) error {
	if err := k.DeleteToken(ctx, name); err != nil {
		return err
	}
	for deadline := time.Now().Add(20 * time.Second); ; {
		_, found, err := k.token(ctx, name)
		if err != nil {
			return err
		}
		if !found {
			break
		}
		if time.Now().After(deadline) {
			return errors.New("the old token is still being deleted")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return k.do(ctx, http.MethodPost, fmt.Sprintf(tokensPath, url.PathEscape(k.Namespace)), TokenObject(k.Namespace, name, bot, publicKey), nil)
}

// DeleteToken deletes a token the hub made.
func (k *Kube) DeleteToken(ctx context.Context, name string) error {
	meta, found, err := k.token(ctx, name)
	if err != nil || !found {
		return err
	}
	if meta.Labels[ManagedByLabel] != ManagedBy {
		return ErrNotOurs
	}
	if meta.DeletionTimestamp != nil {
		return nil
	}
	err = k.do(ctx, http.MethodDelete, fmt.Sprintf(tokensPath, url.PathEscape(k.Namespace))+"/"+url.PathEscape(name), nil, nil)
	if statusOf(err) == http.StatusNotFound {
		return nil
	}
	return err
}
