package mcpconn

// localCatalog is the local servers written by hand; the bulk is localMoreCatalog in catalog_more.go: open
// source MCP servers the runtime starts with npx or uvx at a pinned version, their keys typed by the person into Env.
var localCatalog = []CatalogEntry{
	{ID: "microsoft-365", Name: "Microsoft 365", Kind: KindMail, Transport: Stdio, Auth: AuthNone, Runtime: "npx",
		Command: []string{"npx", "-y", "@softeria/ms-365-mcp-server@0.187.0"},
		Env: []EnvField{{Name: "MS365_MCP_CLIENT_ID", Label: "Client ID своего приложения Entra", Optional: true, Hint: "не нужен: по умолчанию работает общее приложение"},
			{Name: "MS365_MCP_TENANT_ID", Label: "Tenant ID", Optional: true, Hint: "для рабочего аккаунта организации"},
			{Name: "MS365_MCP_CLIENT_SECRET", Label: "Client secret своего приложения", Secret: true, Optional: true}},
		Guide:    "Sign-in is by device code: call its login tool, give the person the URL and the code it returns, then verify-login. The sign-in is kept on the machine.",
		TokenURL: "https://entra.microsoft.com/", DocsURL: "https://github.com/Softeria/ms-365-mcp-server",
		Blurb:   "Outlook mail and calendar, OneDrive, Word, Excel, PowerPoint, Teams, To Do and Planner through Microsoft Graph",
		BlurbRu: "Почта и календарь Outlook, OneDrive, Word, Excel, PowerPoint, Teams, To Do и Planner через Microsoft Graph"},
	{ID: "google-analytics", Name: "Google Analytics 4", Kind: KindSEO, Transport: Stdio, Auth: AuthEnv, Runtime: "uvx",
		Command: []string{"uvx", "analytics-mcp==1.1.0"},
		Env: []EnvField{{Name: "GOOGLE_APPLICATION_CREDENTIALS", Label: "JSON-ключ сервисного аккаунта", Secret: true, File: true,
			Hint: "Google Cloud → сервисный аккаунт → Ключи → JSON; вставьте содержимое файла и выдайте email аккаунта доступ «Читатель» в GA4"},
			{Name: "GOOGLE_PROJECT_ID", Label: "ID проекта Google Cloud", Optional: true}},
		TokenURL: "https://console.cloud.google.com/iam-admin/serviceaccounts", DocsURL: "https://github.com/googleanalytics/google-analytics-mcp",
		Blurb:   "Official Google Analytics server: accounts, properties, reports and realtime data of GA4",
		BlurbRu: "Официальный сервер Google Analytics: аккаунты, ресурсы, отчёты и данные в реальном времени GA4"},
}
