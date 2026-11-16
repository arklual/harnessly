package mcpconn

// Kind groups the catalog on the page: what part of the person's work a server reaches.
type Kind string

const (
	KindMail       Kind = "mail"
	KindCalendar   Kind = "calendar"
	KindMeetings   Kind = "meetings"
	KindTasks      Kind = "tasks"
	KindDocs       Kind = "docs"
	KindCode       Kind = "code"
	KindMonitoring Kind = "monitoring"
	KindSupport    Kind = "support"
	KindPayments   Kind = "payments"
	KindSites      Kind = "sites"
)

// Kinds is the order of the catalog's groups on the page.
var Kinds = []Kind{KindMail, KindCalendar, KindMeetings, KindTasks, KindDocs, KindCode, KindMonitoring, KindSupport,
	KindPayments, KindSites}

// CatalogEntry is a well-known remote MCP server the page offers. Its URL and auth are a starting point only:
// connecting always probes, so a stale hint corrects itself. Every entry is the vendor's own hosted server, its address
// and sign-in checked against the vendor's docs and a live answer of the endpoint. Only what connects today: a server
// that lets in only the clients of its own list (Figma, Square), or only a client registered beforehand with no token
// to type instead (Asana), does not belong here, since ours would never sign in.
type CatalogEntry struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Kind      Kind      `json:"kind"`
	URL       string    `json:"url"`
	Transport Transport `json:"transport"`
	// Auth is the expected sign-in: "oauth", "header" or "none". "header" on a server that answers initialize
	// without one (Yandex 360) still asks the person for the token: its tools refuse without it.
	Auth Auth `json:"auth"`
	// HeaderName presets the header of a typed credential (optional for auth "none").
	HeaderName string `json:"headerName,omitempty"`
	// TokenPrefix goes before a bare token in Authorization instead of "Bearer " ("OAuth " for Yandex 360,
	// "Token token=" for a PagerDuty user token).
	TokenPrefix string `json:"tokenPrefix,omitempty"`
	// TokenURL is where the person creates the token for a header.
	TokenURL string `json:"tokenUrl,omitempty"`
	DocsURL  string `json:"docsUrl,omitempty"`
	Blurb    string `json:"blurb"`
	// BlurbRu is one sentence of what an agent can do with the server.
	BlurbRu string `json:"blurbRu"`
}

var catalog = []CatalogEntry{
	// Mail and calendar.
	{ID: "yandex-mail", Name: "Яндекс Почта", Kind: KindMail, URL: "https://mcp.360.yandex.net/v1/mail/mcp", Transport: HTTP,
		Auth: AuthHeader, HeaderName: "Authorization", TokenPrefix: "OAuth ",
		TokenURL: "https://oauth.yandex.ru/authorize?client_id=355734217047470d8064b5dbbf579fee&response_type=token",
		DocsURL:  "https://yandex.ru/dev/yandex-360/mcp/ru/mail",
		Blurb:    "Finds and reads mail, sorts it into folders and labels, drafts replies; needs a Yandex 360 subscription",
		BlurbRu:  "Найдёт и прочитает письма, разложит по папкам и меткам, подготовит черновик ответа; нужна подписка Яндекс 360"},
	{ID: "yandex-calendar", Name: "Яндекс Календарь", Kind: KindCalendar, URL: "https://mcp.360.yandex.net/v1/calendar/mcp",
		Transport: HTTP, Auth: AuthHeader, HeaderName: "Authorization", TokenPrefix: "OAuth ",
		TokenURL: "https://oauth.yandex.ru/authorize?client_id=d5fff53202474631801939f538932a5c&response_type=token",
		DocsURL:  "https://yandex.ru/dev/yandex-360/mcp/ru/calendar",
		Blurb:    "Reads your schedule and free time, creates and moves meetings; needs a Yandex 360 subscription",
		BlurbRu:  "Посмотрит расписание и свободное время, создаст и перенесёт встречу; нужна подписка Яндекс 360"},
	{ID: "calendly", Name: "Calendly", Kind: KindCalendar, URL: "https://mcp.calendly.com", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://developer.calendly.com/calendly-mcp-server",
		Blurb:   "Sees booked meetings and event types, shares booking links and cancels meetings",
		BlurbRu: "Покажет назначенные встречи и типы событий, даст ссылку на запись и отменит встречу"},
	{ID: "calcom", Name: "Cal.com", Kind: KindCalendar, URL: "https://mcp.cal.com/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://cal.com/docs/mcp-server",
		Blurb:   "Sees bookings and availability, books, moves and cancels meetings",
		BlurbRu: "Покажет брони и свободные слоты, забронирует, перенесёт и отменит встречу"},
	// Meetings.
	{ID: "granola", Name: "Granola", Kind: KindMeetings, URL: "https://mcp.granola.ai/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://docs.granola.ai/help-center/sharing/integrations/mcp",
		Blurb:   "Reads your meeting notes and transcripts and answers questions about them",
		BlurbRu: "Прочитает заметки и расшифровки ваших встреч и ответит по ним"},
	{ID: "fireflies", Name: "Fireflies", Kind: KindMeetings, URL: "https://api.fireflies.ai/mcp", Transport: HTTP,
		Auth: AuthHeader, HeaderName: "Authorization",
		DocsURL: "https://docs.fireflies.ai/getting-started/mcp-configuration",
		Blurb:   "Searches meeting transcripts and summaries, finds who said what",
		BlurbRu: "Найдёт нужное в расшифровках и итогах встреч, подскажет, кто что сказал"},
	// Tasks.
	{ID: "atlassian", Name: "Atlassian", Kind: KindTasks, URL: "https://mcp.atlassian.com/v2/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://support.atlassian.com/atlassian-rovo-mcp-server/docs/getting-started-with-the-atlassian-remote-mcp-server/",
		Blurb:   "Finds, creates and updates Jira issues, reads and writes Confluence pages",
		BlurbRu: "Найдёт, заведёт и обновит задачи Jira, прочитает и поправит страницы Confluence"},
	{ID: "linear", Name: "Linear", Kind: KindTasks, URL: "https://mcp.linear.app/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://linear.app/docs/mcp",
		Blurb:   "Finds, creates and updates issues, projects and cycles",
		BlurbRu: "Найдёт, заведёт и обновит задачи, проекты и циклы"},
	{ID: "todoist", Name: "Todoist", Kind: KindTasks, URL: "https://ai.todoist.net/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://github.com/Doist/todoist-ai",
		Blurb:   "Reads your task lists, adds tasks, sets due dates and completes them",
		BlurbRu: "Посмотрит списки дел, добавит задачу, поставит срок и отметит сделанное"},
	{ID: "monday", Name: "monday.com", Kind: KindTasks, URL: "https://mcp.monday.com/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://developer.monday.com/apps/docs/mondaycom-mcp-integration",
		Blurb:   "Reads boards and items, creates items and posts updates",
		BlurbRu: "Прочитает доски и элементы, заведёт элемент и напишет обновление"},
	// Docs and data.
	{ID: "notion", Name: "Notion", Kind: KindDocs, URL: "https://mcp.notion.com/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://developers.notion.com/docs/mcp",
		Blurb:   "Searches, reads and edits pages and databases of your workspace",
		BlurbRu: "Найдёт, прочитает и поправит страницы и базы рабочего пространства"},
	{ID: "airtable", Name: "Airtable", Kind: KindDocs, URL: "https://mcp.airtable.com/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://airtable.com/developers/agents/mcp/getting-started",
		Blurb:   "Reads bases and tables, adds and updates records",
		BlurbRu: "Прочитает базы и таблицы, добавит и обновит записи"},
	// Code and infrastructure.
	{ID: "github", Name: "GitHub", Kind: KindCode, URL: "https://api.githubcopilot.com/mcp/", Transport: HTTP, Auth: AuthHeader,
		HeaderName: "Authorization", TokenURL: "https://github.com/settings/personal-access-tokens/new",
		DocsURL: "https://github.com/github/github-mcp-server",
		Blurb:   "Reads repositories, works with issues and pull requests",
		BlurbRu: "Прочитает репозитории, поработает с задачами и пул-реквестами"},
	{ID: "gitlab", Name: "GitLab", Kind: KindCode, URL: "https://gitlab.com/api/v4/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://docs.gitlab.com/user/gitlab_duo/model_context_protocol/mcp_server/",
		Blurb:   "Reads projects, issues, merge requests and pipelines on gitlab.com",
		BlurbRu: "Прочитает проекты, задачи, merge request'ы и пайплайны на gitlab.com"},
	{ID: "vercel", Name: "Vercel", Kind: KindCode, URL: "https://mcp.vercel.com", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://vercel.com/docs/mcp/vercel-mcp",
		Blurb:   "Sees projects and deployments, reads build and runtime logs",
		BlurbRu: "Покажет проекты и деплои, прочитает логи сборки и работы"},
	{ID: "supabase", Name: "Supabase", Kind: KindCode, URL: "https://mcp.supabase.com/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://supabase.com/docs/guides/getting-started/mcp",
		Blurb:   "Queries databases, changes tables and deploys edge functions",
		BlurbRu: "Сделает запрос к базе, поправит таблицы и выложит edge-функции"},
	{ID: "cloudflare", Name: "Cloudflare", Kind: KindCode, URL: "https://mcp.cloudflare.com/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://developers.cloudflare.com/agents/model-context-protocol/mcp-servers-for-cloudflare/",
		Blurb:   "Works with your Cloudflare account: Workers, DNS, storage and the rest of its API",
		BlurbRu: "Поработает с аккаунтом Cloudflare: Workers, DNS, хранилища и остальной API"},
	{ID: "cloudflare-docs", Name: "Cloudflare Docs", Kind: KindCode, URL: "https://docs.mcp.cloudflare.com/mcp", Transport: HTTP,
		Auth:    AuthNone,
		DocsURL: "https://developers.cloudflare.com/agents/model-context-protocol/mcp-servers-for-cloudflare/",
		Blurb:   "Searches the Cloudflare documentation",
		BlurbRu: "Найдёт ответ в документации Cloudflare"},
	{ID: "context7", Name: "Context7", Kind: KindCode, URL: "https://mcp.context7.com/mcp", Transport: HTTP, Auth: AuthNone,
		HeaderName: "CONTEXT7_API_KEY", TokenURL: "https://context7.com/dashboard",
		DocsURL: "https://github.com/upstash/context7",
		Blurb:   "Pulls up-to-date docs of libraries and frameworks",
		BlurbRu: "Подтянет свежую документацию библиотек и фреймворков"},
	{ID: "huggingface", Name: "Hugging Face", Kind: KindCode, URL: "https://huggingface.co/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://huggingface.co/docs/hub/agents-mcp",
		Blurb:   "Finds models, datasets and Spaces",
		BlurbRu: "Найдёт модели, датасеты и Spaces"},
	// Monitoring.
	{ID: "sentry", Name: "Sentry", Kind: KindMonitoring, URL: "https://mcp.sentry.dev/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://docs.sentry.io/product/sentry-mcp/",
		Blurb:   "Finds errors and issues, reads their stack traces and releases",
		BlurbRu: "Найдёт ошибки и инциденты, разберёт стектрейсы и релизы"},
	{ID: "pagerduty", Name: "PagerDuty", Kind: KindMonitoring, URL: "https://mcp.pagerduty.com/mcp", Transport: HTTP,
		Auth: AuthHeader, HeaderName: "Authorization", TokenPrefix: "Token token=",
		DocsURL: "https://support.pagerduty.com/main/docs/pagerduty-mcp-server-integration-guide",
		Blurb:   "Sees incidents, on-call schedules and services; works with a user API token",
		BlurbRu: "Покажет инциденты, дежурства и сервисы; нужен пользовательский API-токен"},
	{ID: "posthog", Name: "PostHog", Kind: KindMonitoring, URL: "https://mcp.posthog.com/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://posthog.com/docs/model-context-protocol",
		Blurb:   "Queries product analytics, feature flags, experiments and errors",
		BlurbRu: "Посчитает продуктовую аналитику, покажет флаги, эксперименты и ошибки"},
	// Support.
	{ID: "intercom", Name: "Intercom", Kind: KindSupport, URL: "https://mcp.intercom.com/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://developers.intercom.com/docs/guides/mcp",
		Blurb:   "Searches conversations, contacts and tickets",
		BlurbRu: "Найдёт диалоги, контакты и тикеты"},
	// Payments.
	{ID: "stripe", Name: "Stripe", Kind: KindPayments, URL: "https://mcp.stripe.com", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://docs.stripe.com/mcp",
		Blurb:   "Sees payments and customers, searches the Stripe docs",
		BlurbRu: "Покажет платежи и клиентов, найдёт ответ в документации Stripe"},
	{ID: "paypal", Name: "PayPal", Kind: KindPayments, URL: "https://mcp.paypal.com/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://developer.paypal.com/tools/mcp-server/",
		Blurb:   "Creates invoices and orders, sees transactions",
		BlurbRu: "Выставит счёт, создаст заказ и покажет транзакции"},
	// Sites.
	{ID: "webflow", Name: "Webflow", Kind: KindSites, URL: "https://mcp.webflow.com/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://developers.webflow.com/mcp/reference/overview",
		Blurb:   "Edits sites, pages and CMS collections",
		BlurbRu: "Поправит сайты, страницы и коллекции CMS"},
	{ID: "wix", Name: "Wix", Kind: KindSites, URL: "https://mcp.wix.com/mcp", Transport: HTTP, Auth: AuthOAuth,
		DocsURL: "https://dev.wix.com/docs/sdk/articles/use-the-wix-mcp/about-the-wix-mcp",
		Blurb:   "Manages your Wix sites: pages, store, bookings and other site data",
		BlurbRu: "Поработает с сайтами Wix: страницы, магазин, бронирования и другие данные сайта"},
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

// CatalogName is the name the catalog gives the entry id; the id itself for one it does not know.
func CatalogName(id string) string {
	if e, ok := catalogEntry(id); ok && e.Name != "" {
		return e.Name
	}
	return id
}

// KnownCatalogID says whether id is one of the catalog's entries.
func KnownCatalogID(id string) bool {
	_, ok := catalogEntry(id)
	return ok
}

// ByCatalog is the person's server made from the catalog entry id, as agents may use it now: turned on and signed in.
// ok is false when the person has no such server, or it waits for a sign-in. The servers are the machine's, so two
// people on two machines each get their own.
func (s *Service) ByCatalog(id string) (Server, bool) {
	if id == "" {
		return Server{}, false
	}
	for _, srv := range s.Enabled() {
		if srv.CatalogID == id {
			return srv, true
		}
	}
	return Server{}, false
}
