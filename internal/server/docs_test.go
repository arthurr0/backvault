package server

import (
	"io/fs"
	"net/http"
	"strings"
	"testing"

	docsfs "github.com/arthurr0/backvault/docs"
)

func docsMarkdownFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	err := fs.WalkDir(docsfs.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(name, ".md") {
			out = append(out, name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDocsIndexCoversEveryPageInReadmeOrder(t *testing.T) {
	env := newEnv(t)
	var body docsIndexResponse
	resp := env.call("GET", "/api/v1/docs", nil, &body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("docs index status = %d", resp.StatusCode)
	}
	files := docsMarkdownFiles(t)
	if len(body.Items) != len(files) {
		t.Fatalf("index has %d pages, the embedded docs have %d", len(body.Items), len(files))
	}
	seen := map[string]docsIndexItem{}
	for _, item := range body.Items {
		seen[item.Path] = item
	}
	for _, file := range files {
		item, ok := seen[file]
		if !ok {
			t.Fatalf("%s is missing from the docs index", file)
		}
		if item.Title == "" {
			t.Fatalf("%s has no title", file)
		}
		if item.Section == "" {
			t.Fatalf("%s has no section", file)
		}
	}

	wantSections := []string{
		"Overview", "Start here", "Configuring Backvault", "Drivers", "Running backups", "Reference", "Other",
	}
	if len(body.Sections) != len(wantSections) {
		t.Fatalf("sections = %v, want %v", body.Sections, wantSections)
	}
	for i, name := range wantSections {
		if body.Sections[i] != name {
			t.Fatalf("section %d = %q, want %q", i, body.Sections[i], name)
		}
	}

	wantOrder := []string{
		"README.md", "introduction.md", "install.md", "first-backup.md", "concepts.md",
		"configuration.md", "retention.md", "encryption.md", "notifications.md", "security.md",
		"sources/README.md", "destinations/README.md",
	}
	for i, want := range wantOrder {
		if body.Items[i].Path != want {
			t.Fatalf("page %d = %q, want %q", i, body.Items[i].Path, want)
		}
	}
	if body.Items[len(body.Items)-1].Section != "Other" {
		t.Fatalf("last page section = %q, want Other", body.Items[len(body.Items)-1].Section)
	}

	var postgres docsIndexItem
	for _, item := range body.Items {
		if item.Path == "sources/postgres.md" {
			postgres = item
		}
	}
	if postgres.Section != "Drivers" {
		t.Fatalf("postgres section = %q, want Drivers", postgres.Section)
	}
	if postgres.Title != "PostgreSQL" {
		t.Fatalf("postgres title = %q", postgres.Title)
	}
	if postgres.Description == "" {
		t.Fatal("postgres has no description")
	}
	if len(postgres.Headings) == 0 {
		t.Fatal("postgres has no headings")
	}
	for _, heading := range postgres.Headings {
		if heading.Level != 2 && heading.Level != 3 {
			t.Fatalf("heading level = %d, want 2 or 3", heading.Level)
		}
		if heading.ID == "" {
			t.Fatalf("heading %q has no id", heading.Text)
		}
	}
	if postgres.Headings[0].ID != "configuration-fields" {
		t.Fatalf("first heading id = %q, want configuration-fields", postgres.Headings[0].ID)
	}
}

func TestDocsPageAndNeighbours(t *testing.T) {
	env := newEnv(t)
	var page docsPageResponse
	resp := env.call("GET", "/api/v1/docs/page?path=sources/postgres.md", nil, &page)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("docs page status = %d", resp.StatusCode)
	}
	if page.Title != "PostgreSQL" || page.Section != "Drivers" {
		t.Fatalf("page = %+v", page)
	}
	if !strings.HasPrefix(page.Markdown, "# PostgreSQL") {
		t.Fatalf("markdown does not start with the title: %.40q", page.Markdown)
	}
	if page.Words == 0 {
		t.Fatal("word count is zero")
	}
	if page.Prev == nil || page.Prev.Path != "destinations/hetzner-storage-box.md" {
		t.Fatalf("prev = %+v", page.Prev)
	}
	if page.Next == nil || page.Next.Path != "sources/mysql.md" {
		t.Fatalf("next = %+v", page.Next)
	}

	var first docsPageResponse
	env.call("GET", "/api/v1/docs/page?path=README.md", nil, &first)
	if first.Prev != nil {
		t.Fatalf("the index page has a previous page: %+v", first.Prev)
	}
	if first.Next == nil || first.Next.Path != "introduction.md" {
		t.Fatalf("next after the index = %+v", first.Next)
	}
}

func TestDocsPageRejectsBadPaths(t *testing.T) {
	env := newEnv(t)
	for _, bad := range []string{"../go.mod", "sources/../../go.mod", "install", "", "sources/postgres.md/../../go.mod"} {
		resp := env.do(env.request("GET", "/api/v1/docs/page?path="+bad, nil))
		if resp.StatusCode != http.StatusBadRequest {
			resp.Body.Close()
			t.Fatalf("path %q status = %d, want 400", bad, resp.StatusCode)
		}
		body := errorBody(t, resp)
		if body.Code != codeValidation {
			t.Fatalf("path %q code = %q", bad, body.Code)
		}
	}
	resp := env.do(env.request("GET", "/api/v1/docs/page?path=nope.md", nil))
	if resp.StatusCode != http.StatusNotFound {
		resp.Body.Close()
		t.Fatalf("unknown page status = %d, want 404", resp.StatusCode)
	}
	if body := errorBody(t, resp); body.Code != codeNotFound {
		t.Fatalf("unknown page code = %q", body.Code)
	}
}

func TestDocsSearchRanksTitlesFirst(t *testing.T) {
	env := newEnv(t)
	var found docsSearchResponse
	env.call("GET", "/api/v1/docs/search?q=postgresql", nil, &found)
	if len(found.Items) == 0 {
		t.Fatal("no results for postgresql")
	}
	if found.Items[0].Path != "sources/postgres.md" {
		t.Fatalf("first result = %q, want sources/postgres.md", found.Items[0].Path)
	}
	if found.Items[0].Snippet == "" {
		t.Fatal("first result has no snippet")
	}
	if found.Items[0].Score <= found.Items[len(found.Items)-1].Score && len(found.Items) > 1 {
		t.Fatalf("results are not ranked: %+v", found.Items)
	}
	if len(found.Items) > 20 {
		t.Fatalf("search returned %d results", len(found.Items))
	}

	var retention docsSearchResponse
	env.call("GET", "/api/v1/docs/search?q=retention", nil, &retention)
	if len(retention.Items) == 0 || retention.Items[0].Path != "retention.md" {
		t.Fatalf("retention results = %+v", retention.Items)
	}

	var empty docsSearchResponse
	env.call("GET", "/api/v1/docs/search?q=", nil, &empty)
	if len(empty.Items) != 0 {
		t.Fatalf("empty query returned %d results", len(empty.Items))
	}

	var none docsSearchResponse
	env.call("GET", "/api/v1/docs/search?q=qzxwvnothinghere", nil, &none)
	if len(none.Items) != 0 {
		t.Fatalf("nonsense query returned %d results", len(none.Items))
	}
}

func TestDocsAssetsAreServed(t *testing.T) {
	env := newEnv(t)
	resp := env.do(env.request("GET", "/api/v1/docs/assets/images/dashboard.png", nil))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("asset status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "image/png" {
		t.Fatalf("content type = %q", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "public, max-age=3600" {
		t.Fatalf("cache control = %q", got)
	}

	for _, bad := range []string{
		"/api/v1/docs/assets/install.md",
		"/api/v1/docs/assets/images/nope.png",
		"/api/v1/docs/assets/",
	} {
		other := env.do(env.request("GET", bad, nil))
		other.Body.Close()
		if other.StatusCode != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", bad, other.StatusCode)
		}
	}
}

func TestDocsEndpointsNeedNoAuth(t *testing.T) {
	env := newEnv(t)
	for _, path := range []string{
		"/api/v1/docs",
		"/api/v1/docs/page?path=install.md",
		"/api/v1/docs/search?q=install",
		"/api/v1/docs/assets/images/dashboard.png",
	} {
		resp := env.do(env.request("GET", path, nil))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s without a session = %d, want 200", path, resp.StatusCode)
		}
	}
}
