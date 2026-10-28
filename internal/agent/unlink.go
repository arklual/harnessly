package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

const unlinkTimeout = 20 * time.Second

// Unlink asks the hub to remove this machine, with the signature of the machine's own key (DELETE /agent/v1 on the
// hub that the dial URL points at). It returns nil when the hub no longer has the machine, whether it was removed now
// or was gone before. The files of the machine stay.
func Unlink(ctx context.Context, m *MachineFile, client *http.Client, now time.Time) error {
	host, err := hubproto.DialHost(m.DialURL)
	if err != nil {
		return err
	}
	u, err := url.Parse(m.DialURL)
	if err != nil {
		return err
	}
	switch u.Scheme {
	case "wss":
		u.Scheme = "https"
	case "ws":
		u.Scheme = "http"
	}
	auth, err := hubproto.SignUnlink(m.SigningKey(), m.MachineID, now, host)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, unlinkTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", auth)
	if client == nil {
		client = &http.Client{Timeout: unlinkTimeout}
	}
	res, err := client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("could not reach the hub at %s: %w", m.HubURL, err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
	switch res.StatusCode {
	case http.StatusNoContent, http.StatusOK, http.StatusForbidden:
		// 403 is a machine that was removed in the hub already.
		return nil
	case http.StatusUnauthorized:
		return errors.New("the hub did not accept this machine's signature: check the clock, or the machine is not in this hub")
	}
	return fmt.Errorf("the hub did not take the request: HTTP %d", res.StatusCode)
}

func cmdUnlink(paths cliPaths, stdout, stderr io.Writer) int {
	m, err := LoadMachine(paths.machineFile)
	switch {
	case errors.Is(err, os.ErrNotExist):
		fmt.Fprintln(stdout, "not registered: nothing to remove from the hub")
		return 0
	case err != nil:
		fmt.Fprintln(stderr, "harnessly-agent unlink:", err)
		return 1
	}
	if err := Unlink(context.Background(), m, nil, time.Now()); err != nil {
		fmt.Fprintln(stderr, "harnessly-agent unlink:", strings.TrimSpace(err.Error()))
		return 1
	}
	fmt.Fprintf(stdout, "unlinked: %s is no longer in %s\n", m.MachineID, m.HubURL)
	return 0
}
