package docs

import (
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const (
	otherSection       = "Other"
	overviewSection    = "Overview"
	indexPath          = "README.md"
	assetPrefix        = "images/"
	descriptionLimit   = 220
	snippetRadius      = 60
	defaultSearchLimit = 20
)

type Heading struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	ID    string `json:"id"`
}

type Page struct {
	Path        string    `json:"path"`
	Title       string    `json:"title"`
	Section     string    `json:"section"`
	Description string    `json:"description"`
	Headings    []Heading `json:"headings"`
	Words       int       `json:"words"`

	markdown string
	plain    string
	order    int
}

func (p *Page) Markdown() string { return p.markdown }

type Link struct {
	Path  string `json:"path"`
	Title string `json:"title"`
}

type SearchHit struct {
	Path    string `json:"path"`
	Title   string `json:"title"`
	Section string `json:"section"`
	Snippet string `json:"snippet"`
	Score   int    `json:"score"`
}

type Catalogue struct {
	fsys     fs.FS
	pages    []*Page
	byPath   map[string]*Page
	sections []string
}

func Load(fsys fs.FS) (*Catalogue, error) {
	c := &Catalogue{fsys: fsys, byPath: map[string]*Page{}}
	err := fs.WalkDir(fsys, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(name, ".md") {
			return nil
		}
		body, readErr := fs.ReadFile(fsys, name)
		if readErr != nil {
			return readErr
		}
		page := parsePage(name, string(body))
		c.pages = append(c.pages, page)
		c.byPath[name] = page
		return nil
	})
	if err != nil {
		return nil, err
	}
	c.applyIndexOrder()
	return c, nil
}

type placement struct {
	section string
	order   int
}

func (c *Catalogue) applyIndexOrder() {
	sections := []string{overviewSection}
	placements := map[string]placement{}
	order := 0
	if index, ok := c.byPath[indexPath]; ok {
		placements[index.Path] = placement{section: overviewSection, order: order}
		order++
	}
	if index, ok := c.byPath[indexPath]; ok {
		current := ""
		for _, line := range strings.Split(index.markdown, "\n") {
			if heading := headingOf(line); heading != nil && heading.Level == 2 {
				current = heading.Text
				continue
			}
			for _, target := range linkTargets(line) {
				resolved := resolveDocPath("", target)
				if resolved == "" {
					continue
				}
				if _, known := c.byPath[resolved]; !known {
					continue
				}
				if _, taken := placements[resolved]; taken {
					continue
				}
				section := current
				if section == "" {
					section = otherSection
				}
				if !containsString(sections, section) {
					sections = append(sections, section)
				}
				placements[resolved] = placement{section: section, order: order}
				order++
			}
		}
	}
	hasOther := false
	for _, page := range c.pages {
		if spot, ok := placements[page.Path]; ok {
			page.Section = spot.section
			page.order = spot.order
			continue
		}
		page.Section = otherSection
		page.order = order
		order++
		hasOther = true
	}
	if hasOther && !containsString(sections, otherSection) {
		sections = append(sections, otherSection)
	}
	rank := map[string]int{}
	for i, name := range sections {
		rank[name] = i
	}
	sort.SliceStable(c.pages, func(i, j int) bool {
		left, right := c.pages[i], c.pages[j]
		if rank[left.Section] != rank[right.Section] {
			return rank[left.Section] < rank[right.Section]
		}
		return left.order < right.order
	})
	kept := make([]string, 0, len(sections))
	for _, name := range sections {
		for _, page := range c.pages {
			if page.Section == name {
				kept = append(kept, name)
				break
			}
		}
	}
	c.sections = kept
}

func (c *Catalogue) Pages() []*Page { return c.pages }

func (c *Catalogue) Sections() []string { return c.sections }

func (c *Catalogue) Lookup(name string) (*Page, bool) {
	page, ok := c.byPath[name]
	return page, ok
}

func (c *Catalogue) Neighbours(name string) (*Link, *Link) {
	position := -1
	for i, page := range c.pages {
		if page.Path == name {
			position = i
			break
		}
	}
	if position < 0 {
		return nil, nil
	}
	var prev, next *Link
	if position > 0 {
		earlier := c.pages[position-1]
		prev = &Link{Path: earlier.Path, Title: earlier.Title}
	}
	if position < len(c.pages)-1 {
		later := c.pages[position+1]
		next = &Link{Path: later.Path, Title: later.Title}
	}
	return prev, next
}

func (c *Catalogue) Asset(name string) (fs.File, string, bool) {
	cleaned := path.Clean("/" + name)[1:]
	if cleaned == "" || strings.Contains(name, "..") || !strings.HasPrefix(cleaned, assetPrefix) {
		return nil, "", false
	}
	file, err := c.fsys.Open(cleaned)
	if err != nil {
		return nil, "", false
	}
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		_ = file.Close()
		return nil, "", false
	}
	return file, assetContentType(cleaned), true
}

func (c *Catalogue) Search(query string, limit int) []SearchHit {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return []SearchHit{}
	}
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	tokens := strings.Fields(needle)
	type scored struct {
		page  *Page
		score int
	}
	var matches []scored
	for _, page := range c.pages {
		title := strings.ToLower(page.Title)
		headings := strings.ToLower(headingText(page.Headings))
		body := strings.ToLower(page.plain)
		score := 0
		if strings.Contains(title, needle) {
			score += 120
			if title == needle {
				score += 60
			}
		}
		if strings.Contains(headings, needle) {
			score += 45
		}
		if strings.Contains(body, needle) {
			score += 12
		}
		covered := true
		for _, token := range tokens {
			hit := false
			if strings.Contains(title, token) {
				score += 30
				hit = true
			}
			if strings.Contains(headings, token) {
				score += 10
				hit = true
			}
			if strings.Contains(body, token) {
				score += 3
				hit = true
			}
			if strings.Contains(strings.ToLower(page.Path), token) {
				score += 5
				hit = true
			}
			if !hit {
				covered = false
				break
			}
		}
		if !covered || score == 0 {
			continue
		}
		matches = append(matches, scored{page: page, score: score})
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].page.order < matches[j].page.order
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	hits := make([]SearchHit, 0, len(matches))
	for _, match := range matches {
		hits = append(hits, SearchHit{
			Path:    match.page.Path,
			Title:   match.page.Title,
			Section: match.page.Section,
			Snippet: snippetFor(match.page, needle, tokens),
			Score:   match.score,
		})
	}
	return hits
}

func headingText(headings []Heading) string {
	parts := make([]string, 0, len(headings))
	for _, heading := range headings {
		parts = append(parts, heading.Text)
	}
	return strings.Join(parts, " \n ")
}

func snippetFor(page *Page, needle string, tokens []string) string {
	body := page.plain
	lower := strings.ToLower(body)
	at := strings.Index(lower, needle)
	width := len(needle)
	if at < 0 {
		for _, token := range tokens {
			if found := strings.Index(lower, token); found >= 0 {
				at = found
				width = len(token)
				break
			}
		}
	}
	if at < 0 {
		if len(page.Description) > 0 {
			return page.Description
		}
		return truncate(body, descriptionLimit)
	}
	start := at - snippetRadius
	if start < 0 {
		start = 0
	}
	end := at + width + snippetRadius
	if end > len(body) {
		end = len(body)
	}
	for start > 0 && body[start] != ' ' {
		start--
	}
	for end < len(body) && body[end] != ' ' {
		end++
	}
	snippet := strings.TrimSpace(body[start:end])
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(body) {
		snippet += "..."
	}
	return snippet
}

var (
	linkPattern    = regexp.MustCompile(`\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	headingPattern = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)
	fencePattern   = regexp.MustCompile("^\\s{0,3}(```|~~~)")
)

type parsedHeading struct {
	Level int
	Text  string
}

func headingOf(line string) *parsedHeading {
	match := headingPattern.FindStringSubmatch(line)
	if match == nil {
		return nil
	}
	return &parsedHeading{Level: len(match[1]), Text: stripInline(match[2])}
}

func linkTargets(line string) []string {
	matches := linkPattern.FindAllStringSubmatch(line, -1)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		out = append(out, match[2])
	}
	return out
}

func resolveDocPath(base, target string) string {
	if target == "" || strings.HasPrefix(target, "#") {
		return ""
	}
	lowered := strings.ToLower(target)
	if strings.HasPrefix(lowered, "http://") || strings.HasPrefix(lowered, "https://") || strings.HasPrefix(lowered, "mailto:") {
		return ""
	}
	if hash := strings.IndexByte(target, '#'); hash >= 0 {
		target = target[:hash]
	}
	if !strings.HasSuffix(target, ".md") {
		return ""
	}
	joined := path.Clean(path.Join(path.Dir(base), target))
	if strings.HasPrefix(joined, "..") {
		return ""
	}
	return joined
}

func parsePage(name, content string) *Page {
	page := &Page{Path: name, Words: len(strings.Fields(content)), markdown: content}
	slugs := map[string]int{}
	lines := strings.Split(content, "\n")
	inFence := false
	var plain []string
	var description []string
	collecting := false
	for _, line := range lines {
		if fencePattern.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			plain = append(plain, strings.TrimSpace(line))
			continue
		}
		heading := headingOf(line)
		if heading != nil {
			if heading.Level == 1 && page.Title == "" {
				page.Title = heading.Text
				collecting = true
			} else if heading.Level == 2 || heading.Level == 3 {
				page.Headings = append(page.Headings, Heading{
					Level: heading.Level,
					Text:  heading.Text,
					ID:    uniqueSlug(slugs, heading.Text),
				})
			}
			if heading.Level > 1 && collecting {
				if len(description) > 0 {
					page.Description = truncate(strings.Join(description, " "), descriptionLimit)
				}
				collecting = false
			}
			plain = append(plain, heading.Text)
			continue
		}
		text := stripInline(line)
		plain = append(plain, text)
		trimmed := strings.TrimSpace(text)
		if collecting && page.Description == "" {
			if trimmed == "" {
				if len(description) > 0 {
					page.Description = truncate(strings.Join(description, " "), descriptionLimit)
					collecting = false
				}
				continue
			}
			if strings.HasPrefix(trimmed, "|") || strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, ">") {
				if len(description) == 0 {
					continue
				}
				page.Description = truncate(strings.Join(description, " "), descriptionLimit)
				collecting = false
				continue
			}
			description = append(description, trimmed)
		}
	}
	if page.Description == "" && len(description) > 0 {
		page.Description = truncate(strings.Join(description, " "), descriptionLimit)
	}
	if page.Title == "" {
		page.Title = strings.TrimSuffix(path.Base(name), ".md")
	}
	page.plain = collapseSpaces(strings.Join(plain, " "))
	return page
}

func collapseSpaces(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func truncate(value string, limit int) string {
	value = collapseSpaces(value)
	if len(value) <= limit {
		return value
	}
	cut := value[:limit]
	if space := strings.LastIndexByte(cut, ' '); space > limit/2 {
		cut = cut[:space]
	}
	return strings.TrimRight(cut, " ,.;:") + "..."
}

var (
	imagePattern  = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	anchorPattern = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	htmlPattern   = regexp.MustCompile(`<[^>]+>`)
)

func stripInline(value string) string {
	value = imagePattern.ReplaceAllString(value, "$1")
	value = anchorPattern.ReplaceAllString(value, "$1")
	value = htmlPattern.ReplaceAllString(value, "")
	value = strings.ReplaceAll(value, "`", "")
	value = strings.ReplaceAll(value, "**", "")
	value = strings.ReplaceAll(value, "~~", "")
	value = strings.ReplaceAll(value, "*", "")
	return strings.TrimSpace(value)
}

func Slug(value string) string {
	var builder strings.Builder
	for _, symbol := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case symbol == ' ' || symbol == '\t' || symbol == '\n':
			builder.WriteRune('-')
		case symbol == '-' || symbol == '_':
			builder.WriteRune(symbol)
		case unicode.IsLetter(symbol) || unicode.IsDigit(symbol):
			builder.WriteRune(symbol)
		}
	}
	return builder.String()
}

func uniqueSlug(seen map[string]int, value string) string {
	base := Slug(value)
	count, taken := seen[base]
	if !taken {
		seen[base] = 0
		return base
	}
	for {
		count++
		candidate := base + "-" + strconv.Itoa(count)
		if _, used := seen[candidate]; !used {
			seen[base] = count
			seen[candidate] = 0
			return candidate
		}
	}
}

func assetContentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".webp":
		return "image/webp"
	case ".avif":
		return "image/avif"
	case ".md":
		return "text/markdown; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
