package main

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/ly-solutions/harnessly/internal/voice/quickedit"
)

func TestQuickToolBody(t *testing.T) {
	body, err := quickToolBody(json.RawMessage(`{"structuredContent":{"text":"a","version":"v1"},"content":[{"type":"text","text":"x"}]}`))
	if err != nil || string(body) != `{"text":"a","version":"v1"}` {
		t.Fatalf("structured: %s %v", body, err)
	}
	body, err = quickToolBody(json.RawMessage(`{"content":[{"type":"text","text":"{\"changed\":1}"}]}`))
	if err != nil || string(body) != `{"changed":1}` {
		t.Fatalf("text: %s %v", body, err)
	}
	_, err = quickToolBody(json.RawMessage(`{"isError":true,"content":[{"type":"text","text":"{\"code\":\"needs_yes\",\"message\":\"removes 2\"}"}]}`))
	var ae *quickedit.AppError
	if !errors.As(err, &ae) || ae.Code != "needs_yes" {
		t.Fatalf("error: %v", err)
	}
	_, err = quickToolBody(json.RawMessage(`{"isError":true,"content":[{"type":"text","text":"boom"}]}`))
	if !errors.As(err, &ae) || ae.Code != "failed" || ae.Message != "boom" {
		t.Fatalf("plain error: %v", err)
	}
}
