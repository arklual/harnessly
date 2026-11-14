package wire

import (
	"context"
	"testing"
	"time"
)

type askedBy struct{}

// the command runs past the request but keeps what the request carries (whose page session sent it).
func TestOpLogKeepsTheRequestValues(t *testing.T) {
	o := NewOpLog("")
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), askedBy{}, "roma@team.test"))
	defer cancel()
	got := ""
	res := o.Run(ctx, ConversationRenameCmd{OpID: "o1", ID: "tab-abc", Title: "x"}, time.Second, func(c context.Context) CmdResult {
		got, _ = c.Value(askedBy{}).(string)
		return Accepted(ChannelConvs, 1)
	})
	if got != "roma@team.test" || res.R == ResultRefused {
		t.Fatalf("value %q, result %+v", got, res)
	}
}
