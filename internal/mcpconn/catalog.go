package mcpconn

// CatalogEntry is a well-known remote MCP server the page offers. Its URL and auth are a starting point only:
// connecting always probes, so a stale hint corrects itself.
type CatalogEntry struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Transport Transport `json:"transport"`
	// Auth is the expected sign-in: "oauth", "header" or "none".
	Auth Auth `json:"auth"`
	// HeaderName presets the header of a typed credential (optional for auth "none").
	HeaderName string `json:"headerName,omitempty"`
	// TokenURL is where the person creates the token for a header.
	TokenURL string `json:"tokenUrl,omitempty"`
	DocsURL  string `json:"docsUrl,omitempty"`
	Blurb    string `json:"blurb"`
	BlurbRu  string `json:"blurbRu"`
}

var catalog = []CatalogEntry{
	{ID: "atlassian", Name: "Atlassian", URL: "https://mcp.atlassian.com/v1/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://support.atlassian.com/rovo/docs/getting-started-with-the-atlassian-remote-mcp-server/",
		Blurb:   "Jira issues and Confluence pages", BlurbRu: "Задачи Jira и страницы Confluence"},
	{ID: "linear", Name: "Linear", URL: "https://mcp.linear.app/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://linear.app/docs/mcp",
		Blurb:   "Issues, projects and cycles", BlurbRu: "Задачи, проекты и циклы"},
	{ID: "notion", Name: "Notion", URL: "https://mcp.notion.com/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://developers.notion.com/docs/mcp",
		Blurb:   "Pages and databases of your workspace", BlurbRu: "Страницы и базы рабочего пространства"},
	{ID: "sentry", Name: "Sentry", URL: "https://mcp.sentry.dev/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://docs.sentry.io/product/sentry-mcp/",
		Blurb:   "Errors, issues and releases", BlurbRu: "Ошибки, инциденты и релизы"},
	{ID: "github", Name: "GitHub", URL: "https://api.githubcopilot.com/mcp/", Transport: HTTP, Auth: AuthHeader,
		HeaderName: "Authorization", TokenURL: "https://github.com/settings/personal-access-tokens/new",
		DocsURL: "https://github.com/github/github-mcp-server",
		Blurb:   "Repositories, issues and pull requests", BlurbRu: "Репозитории, задачи и пул-реквесты"},
	{ID: "asana", Name: "Asana", URL: "https://mcp.asana.com/sse", Transport: SSE, Auth: AuthOAuth,
		DocsURL: "https://developers.asana.com/docs/using-asanas-mcp-server",
		Blurb:   "Tasks and projects", BlurbRu: "Задачи и проекты"},
	{ID: "stripe", Name: "Stripe", URL: "https://mcp.stripe.com", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://docs.stripe.com/mcp",
		Blurb:   "Payments, customers and the Stripe docs", BlurbRu: "Платежи, клиенты и документация Stripe"},
	{ID: "vercel", Name: "Vercel", URL: "https://mcp.vercel.com", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://vercel.com/docs/mcp/vercel-mcp",
		Blurb:   "Projects, deployments and logs", BlurbRu: "Проекты, деплои и логи"},
	{ID: "supabase", Name: "Supabase", URL: "https://mcp.supabase.com/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://supabase.com/docs/guides/getting-started/mcp",
		Blurb:   "Databases, tables and edge functions", BlurbRu: "Базы, таблицы и edge-функции"},
	{ID: "cloudflare-docs", Name: "Cloudflare Docs", URL: "https://docs.mcp.cloudflare.com/mcp", Transport: HTTP, Auth: AuthNone,
		DocsURL: "https://developers.cloudflare.com/agents/model-context-protocol/mcp-servers-for-cloudflare/",
		Blurb:   "Search the Cloudflare documentation", BlurbRu: "Поиск по документации Cloudflare"},
	{ID: "context7", Name: "Context7", URL: "https://mcp.context7.com/mcp", Transport: HTTP, Auth: AuthNone,
		HeaderName: "CONTEXT7_API_KEY", TokenURL: "https://context7.com/dashboard",
		DocsURL: "https://github.com/upstash/context7",
		Blurb:   "Up-to-date docs of libraries and frameworks", BlurbRu: "Свежая документация библиотек и фреймворков"},
	{ID: "huggingface", Name: "Hugging Face", URL: "https://huggingface.co/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://huggingface.co/docs/hub/agents-mcp",
		Blurb:   "Models, datasets and Spaces", BlurbRu: "Модели, датасеты и Spaces"},
	{ID: "figma", Name: "Figma", URL: "https://mcp.figma.com/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://help.figma.com/hc/en-us/articles/32132100833559",
		Blurb:   "Design files, frames and components", BlurbRu: "Макеты, фреймы и компоненты"},
	{ID: "intercom", Name: "Intercom", URL: "https://mcp.intercom.com/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://developers.intercom.com/docs/guides/mcp",
		Blurb:   "Conversations, contacts and tickets", BlurbRu: "Диалоги, контакты и тикеты"},
	{ID: "monday", Name: "monday.com", URL: "https://mcp.monday.com/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://developer.monday.com/apps/docs/mondaycom-mcp-integration",
		Blurb:   "Boards, items and updates", BlurbRu: "Доски, элементы и обновления"},
	{ID: "webflow", Name: "Webflow", URL: "https://mcp.webflow.com/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://developers.webflow.com/mcp/reference/overview",
		Blurb:   "Sites, pages and CMS collections", BlurbRu: "Сайты, страницы и коллекции CMS"},
	{ID: "paypal", Name: "PayPal", URL: "https://mcp.paypal.com/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://developer.paypal.com/tools/mcp-server/",
		Blurb:   "Invoices, orders and transactions", BlurbRu: "Счета, заказы и транзакции"},
	{ID: "square", Name: "Square", URL: "https://mcp.squareup.com/sse", Transport: SSE, Auth: AuthOAuth,
		DocsURL: "https://developer.squareup.com/docs/mcp",
		Blurb:   "Payments, catalog and orders", BlurbRu: "Платежи, каталог и заказы"},
}

func catalogEntry(id string) (CatalogEntry, bool) {
	if id == "" {
		return CatalogEntry{}, false
	}
	for _, e := range catalog {
		if e.ID == id {
			return e, true
		}
	}
	return CatalogEntry{}, false
}
