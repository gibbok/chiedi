package extract

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"rsc.io/pdf"
)

const (
	MaxFileBytes      = 64 << 20
	MaxPDFPages       = 2000
	MaxExtractedBytes = 128 << 20
)

type Section struct {
	Heading   string
	Text      string
	PageStart int
	PageEnd   int
}

type Document struct {
	Title    string
	Sections []Section
}

func File(ctx context.Context, path string) (Document, error) {
	if err := ctx.Err(); err != nil {
		return Document{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Document{}, err
	}
	if info.Size() > MaxFileBytes {
		return Document{}, fmt.Errorf("file exceeds %d-byte limit", MaxFileBytes)
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".txt":
		return textFile(path)
	case ".md":
		return markdownFile(path)
	case ".pdf":
		return pdfFile(ctx, path)
	default:
		return Document{}, errors.New("unsupported file type")
	}
}

func textFile(path string) (Document, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Document{}, err
	}
	if !utf8.Valid(b) {
		return Document{}, errors.New("text is not valid UTF-8")
	}
	text := normalize(string(b))
	return Document{Title: filepath.Base(path), Sections: []Section{{Text: text}}}, nil
}

func markdownFile(path string) (Document, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Document{}, err
	}
	if !utf8.Valid(b) {
		return Document{}, errors.New("markdown is not valid UTF-8")
	}
	lines := strings.Split(normalize(string(b)), "\n")
	title := filepath.Base(path)
	var hierarchy [6]string
	var heading string
	var body []string
	var sections []Section
	inFence := false
	flush := func() {
		text := strings.TrimSpace(strings.Join(body, "\n"))
		if text != "" {
			sections = append(sections, Section{Heading: heading, Text: text})
		}
		body = nil
	}
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			inFence = !inFence
			body = append(body, line)
			continue
		}
		if !inFence {
			level, value := markdownHeading(line)
			if level > 0 {
				flush()
				hierarchy[level-1] = value
				for i := level; i < len(hierarchy); i++ {
					hierarchy[i] = ""
				}
				parts := make([]string, 0, level)
				for i := 0; i < level; i++ {
					if hierarchy[i] != "" {
						parts = append(parts, hierarchy[i])
					}
				}
				heading = strings.Join(parts, " > ")
				if level == 1 && title == filepath.Base(path) {
					title = value
				}
				continue
			}
		}
		body = append(body, line)
	}
	flush()
	return Document{Title: title, Sections: sections}, nil
}

func markdownHeading(line string) (int, string) {
	trim := strings.TrimSpace(line)
	level := 0
	for level < len(trim) && level < 6 && trim[level] == '#' {
		level++
	}
	if level == 0 || level >= len(trim) || trim[level] != ' ' {
		return 0, ""
	}
	return level, strings.TrimSpace(trim[level:])
}

func pdfFile(ctx context.Context, path string) (Document, error) {
	r, err := pdf.Open(path)
	if err != nil {
		return Document{}, fmt.Errorf("parse PDF: %w", err)
	}
	if r.NumPage() > MaxPDFPages {
		return Document{}, fmt.Errorf("PDF exceeds %d-page limit", MaxPDFPages)
	}
	doc := Document{Title: filepath.Base(path)}
	total := 0
	for pageNo := 1; pageNo <= r.NumPage(); pageNo++ {
		if err := ctx.Err(); err != nil {
			return Document{}, err
		}
		page := r.Page(pageNo)
		if page.V.IsNull() {
			continue
		}
		texts := append([]pdf.Text(nil), page.Content().Text...)
		sort.SliceStable(texts, func(i, j int) bool {
			if texts[i].Y == texts[j].Y {
				return texts[i].X < texts[j].X
			}
			return texts[i].Y > texts[j].Y
		})
		var b strings.Builder
		lastY := 0.0
		for i, item := range texts {
			if i > 0 {
				if lastY-item.Y > item.FontSize*.8 {
					b.WriteByte('\n')
				} else {
					b.WriteByte(' ')
				}
			}
			b.WriteString(item.S)
			lastY = item.Y
		}
		text := strings.TrimSpace(b.String())
		total += len(text)
		if total > MaxExtractedBytes {
			return Document{}, errors.New("PDF extracted text exceeds limit")
		}
		if text != "" {
			doc.Sections = append(doc.Sections, Section{Text: text, PageStart: pageNo, PageEnd: pageNo})
		}
	}
	if len(doc.Sections) == 0 {
		return Document{}, errors.New("PDF has no extractable text layer")
	}
	return doc, nil
}

func normalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	var out strings.Builder
	scan := bufio.NewScanner(strings.NewReader(s))
	for scan.Scan() {
		out.WriteString(strings.TrimRight(scan.Text(), " \t"))
		out.WriteByte('\n')
	}
	return strings.TrimSuffix(out.String(), "\n")
}
