package layoutapi

import "github.com/ly-solutions/harnessly/internal/jsvalue"

func (a *api) cliBrowser(c *call) {
	var args *jsvalue.Object
	id := c.agentGuard(func() bool {
		value, ok := c.field("arguments")
		args, _ = value.(*jsvalue.Object)
		if ok && args == nil {
			c.fail(400, "arguments must be an object")
			return false
		}
		if args == nil {
			args = jsvalue.NewObject()
		}
		return true
	}, "This tmux session does not belong to a chat; run the browser command in the chat's terminal")
	if id == "" {
		return
	}
	if a.d.Browser == nil {
		c.fail(503, "Browser tools are unavailable")
		return
	}
	c.reply(200, a.d.Browser(c.r, id, c.param("tool"), args))
}
