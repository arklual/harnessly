package sites

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"time"
)

// A publishing session is what a machine of a space gets from the hub for its signed request: a token that lets the
// machine's runtime make sites in that space and deploy to the ones it made (the «Опубликовать» of a Sinopia project).
// It is signed with the sites key and keeps nothing in the database; every use asks the hub whether the machine is
// still in the space (Authority.MachineOK), so removing the machine ends the session at once.

const (
	machinePrefix = "hsm_"
	// MachineSessionTTL is how long a publishing session lasts.
	MachineSessionTTL = 30 * time.Minute
	// MachineIDPrefix starts the id sites made by a machine carry, apart from every person's id.
	MachineIDPrefix = "machine:"
)

var machineEnc = base64.RawURLEncoding

// MachineSession is who a publishing session stands for.
type MachineSession struct {
	Space     string
	MachineID string
	// PersonID is the member of the space the machine publishes for; empty when the machine did not say.
	PersonID string
	// Name is what is shown as the author: the person's, else the machine's.
	Name string
}

// ID is the stable id of the sites this machine makes.
func (m MachineSession) ID() string { return MachineIDPrefix + m.MachineID }

func (s *Service) machineMAC(payload string) []byte {
	m := hmac.New(sha256.New, s.cfg.Key)
	_, _ = m.Write([]byte("harnessly-sites-machine/v2\x00"))
	_, _ = m.Write([]byte(payload))
	return m.Sum(nil)
}

// NewMachineSession makes the token of a machine for its space; it works until the returned time.
func (s *Service) NewMachineSession(m MachineSession) (string, time.Time) {
	exp := s.now().Add(MachineSessionTTL)
	parts := []string{m.Space, m.MachineID, m.PersonID, m.Name}
	for i, p := range parts {
		parts[i] = machineEnc.EncodeToString([]byte(p))
	}
	payload := strings.Join(parts, ".") + "." + strconv.FormatInt(exp.Unix(), 10)
	return machinePrefix + payload + "." + machineEnc.EncodeToString(s.machineMAC(payload)), exp
}

// machineOf reads a publishing session; it says nothing of whether the machine is still in the space.
func (s *Service) machineOf(token string) (MachineSession, bool) {
	rest, found := strings.CutPrefix(token, machinePrefix)
	if !found {
		return MachineSession{}, false
	}
	parts := strings.Split(rest, ".")
	if len(parts) != 6 {
		return MachineSession{}, false
	}
	payload := strings.Join(parts[:5], ".")
	mac, err := machineEnc.DecodeString(parts[5])
	if err != nil || !hmac.Equal(mac, s.machineMAC(payload)) {
		return MachineSession{}, false
	}
	exp, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil || !s.now().Before(time.Unix(exp, 0)) {
		return MachineSession{}, false
	}
	var f [4]string
	for i := range f {
		b, err := machineEnc.DecodeString(parts[i])
		if err != nil {
			return MachineSession{}, false
		}
		f[i] = string(b)
	}
	return MachineSession{Space: f[0], MachineID: f[1], PersonID: f[2], Name: f[3]}, f[1] != ""
}
