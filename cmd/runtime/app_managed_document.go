package main

import (
	"context"
	"os"
	"strings"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/docprovider"
	"github.com/ly-solutions/harnessly/internal/integrations"
)

const managedDocumentRead = "@host/documents/read"

type documentSubjectKey struct{}

// The operator pins the environment package's actual digest. An install that
// shadows it, including a personal package with the same ID, has no binding.
func documentReaderAllowed(app apps.Installed) bool {
	if !app.Env || app.EnvSum == "" || app.Manifest == nil || !app.Manifest.Allows("documents.read") {
		return false
	}
	for _, entry := range strings.Split(os.Getenv("RUNTIME_DOCUMENT_READER_PACKAGES"), ",") {
		if strings.TrimSpace(entry) == app.ID+"="+app.EnvSum {
			return true
		}
	}
	return false
}

func readManagedDocument(ctx context.Context, session apps.Session, app apps.Installed, args map[string]any) (docprovider.Content, error) {
	var empty docprovider.Content
	owner := strings.TrimSpace(os.Getenv("RUNTIME_OWNER_EMAIL"))
	subject, _ := ctx.Value(documentSubjectKey{}).(string)
	if !documentReaderAllowed(app) || session.ManagedSum == "" || session.ManagedSum != app.EnvSum || !strings.Contains(owner, "@") || !strings.EqualFold(subject, owner) {
		return empty, &apps.BridgeError{Code: apps.CodePermissionDenied, Message: "This view has no personal document reader binding"}
	}
	id, _ := args["id"].(string)
	slug := docprovider.Alias(id)
	if len(args) != 1 || slug == "" {
		return empty, &apps.BridgeError{Code: apps.CodeInvalidParams, Message: "Provide one document id"}
	}
	if !docprovider.Declared() {
		return empty, &apps.BridgeError{Code: apps.CodeHostUnavailable, Message: "Personal documents are not connected"}
	}
	content, err := docprovider.For(integrations.Config{}, nil).Read(ctx, slug, "")
	if err != nil {
		return empty, &apps.BridgeError{Code: apps.CodeUpstreamError, Message: "The personal documents adapter could not read this document"}
	}
	return content, nil
}
