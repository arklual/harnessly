// Package mcpserver is the JSON-RPC of the runtime's MCP server for agents: stateless Streamable HTTP, every POST
// carrying one message (or a batch) and getting its answer as plain JSON, nothing kept between calls. The tools come
// from toolsets (the preview's DevTools, the conversations an agent delegates), each answering for its own names.
package mcpserver

import (
	"context"
	"strconv"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

const (
	serverVersion = "1.0.0"
	maxBatch      = 20

	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
	rpcInternalError  = -32603
)

var protocolVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

// Toolset is a group of tools of the server.
type Toolset interface {
	// Instructions is what the server tells the agent of these tools at initialize.
	Instructions() string
	// Tools are the tools' definitions as tools/list gives them.
	Tools() []any
	Has(name string) bool
	// Call runs a tool the set has with its arguments (an object, empty when none were given): the tools/call result.
	Call(ctx context.Context, name string, args *jsvalue.Object) *jsvalue.Object
}

// Server answers the JSON-RPC messages of its toolsets, in their order.
type Server struct {
	// Name is the name the server announces.
	Name string
	Sets []Toolset
}

func success(id, result any) *jsvalue.Object {
	return jsvalue.NewObject("jsonrpc", "2.0", "id", id, "result", result)
}

func failure(id any, code int, msg string) *jsvalue.Object {
	return jsvalue.NewObject("jsonrpc", "2.0", "id", id, "error", jsvalue.NewObject("code", float64(code), "message", msg))
}

// ParseError is the answer to a body that is not JSON.
func ParseError() []byte { return jsvalue.Stringify(failure(nil, rpcParseError, "Parse error"), "") }

// TextResult is a tools/call result of one text.
func TextResult(text string, isError bool) *jsvalue.Object {
	o := jsvalue.NewObject("content", []any{jsvalue.NewObject("type", "text", "text", text)})
	if isError {
		o.Set("isError", true)
	}
	return o
}

func field(o *jsvalue.Object, k string) any {
	v, _ := o.Get(k)
	return v
}

// Handle answers a parsed POST body: 200 with a body, or 202 without one when there is nothing to answer.
func (s *Server) Handle(ctx context.Context, payload any) (int, []byte) {
	if batch, ok := payload.([]any); ok {
		if len(batch) == 0 || len(batch) > maxBatch {
			return 200, jsvalue.Stringify(failure(nil, rpcInvalidRequest, "A batch holds 1 to "+strconv.Itoa(maxBatch)+" messages"), "")
		}
		var replies []any
		for _, msg := range batch {
			if r := s.message(ctx, msg); r != nil {
				replies = append(replies, r)
			}
		}
		if len(replies) == 0 {
			return 202, nil
		}
		return 200, jsvalue.Stringify(replies, "")
	}
	if r := s.message(ctx, payload); r != nil {
		return 200, jsvalue.Stringify(r, "")
	}
	return 202, nil
}

func (s *Server) message(ctx context.Context, msg any) (reply *jsvalue.Object) {
	o, ok := msg.(*jsvalue.Object)
	if !ok || field(o, "jsonrpc") != "2.0" {
		return failure(nil, rpcInvalidRequest, "Invalid Request")
	}
	method, ok := field(o, "method").(string)
	if !ok {
		if o.Has("result") || o.Has("error") {
			return nil
		}
		return failure(nil, rpcInvalidRequest, "Invalid Request")
	}
	id, present := o.Get("id")
	if !present {
		return nil
	}
	switch id.(type) {
	case string, float64:
	default:
		return failure(nil, rpcInvalidRequest, "Invalid Request: id must be a string or a number")
	}
	defer func() {
		if recover() != nil {
			reply = failure(id, rpcInternalError, "Internal error")
		}
	}()
	params, _ := o.Get("params")
	return s.call(ctx, id, method, params)
}

func (s *Server) instructions() string {
	out := ""
	for _, set := range s.Sets {
		if text := set.Instructions(); text != "" {
			if out != "" {
				out += "\n\n"
			}
			out += text
		}
	}
	return out
}

func (s *Server) tools() []any {
	out := []any{}
	for _, set := range s.Sets {
		out = append(out, set.Tools()...)
	}
	return out
}

func (s *Server) setOf(tool string) Toolset {
	for _, set := range s.Sets {
		if set.Has(tool) {
			return set
		}
	}
	return nil
}

func (s *Server) call(ctx context.Context, id any, method string, params any) *jsvalue.Object {
	p, isRecord := params.(*jsvalue.Object)
	switch method {
	case "initialize":
		asked, ok := any(nil), false
		if isRecord {
			asked, ok = p.Get("protocolVersion")
		}
		version, isString := asked.(string)
		if !isRecord || !ok || !isString {
			return failure(id, rpcInvalidParams, "initialize needs protocolVersion")
		}
		negotiated := protocolVersions[0]
		for _, v := range protocolVersions {
			if v == version {
				negotiated = v
			}
		}
		return success(id, jsvalue.NewObject(
			"protocolVersion", negotiated,
			"capabilities", jsvalue.NewObject("tools", jsvalue.NewObject()),
			"serverInfo", jsvalue.NewObject("name", s.Name, "version", serverVersion),
			"instructions", s.instructions()))
	case "ping":
		return success(id, jsvalue.NewObject())
	case "tools/list":
		return success(id, jsvalue.NewObject("tools", s.tools()))
	case "tools/call":
		name, ok := any(nil), false
		if isRecord {
			name, ok = p.Get("name")
		}
		tool, isString := name.(string)
		if !isRecord || !ok || !isString {
			return failure(id, rpcInvalidParams, "tools/call needs a tool name")
		}
		set := s.setOf(tool)
		if set == nil {
			return failure(id, rpcInvalidParams, "Unknown tool: "+tool)
		}
		args, present := p.Get("arguments")
		if present {
			if _, ok := args.(*jsvalue.Object); !ok {
				return failure(id, rpcInvalidParams, "arguments must be an object")
			}
		}
		obj, _ := args.(*jsvalue.Object)
		if obj == nil {
			obj = jsvalue.NewObject()
		}
		return success(id, set.Call(ctx, tool, obj))
	}
	return failure(id, rpcMethodNotFound, "Method not found: "+method)
}
