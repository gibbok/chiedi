package extract

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/gibbok/chiedi/internal/source"
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
	if !info.Mode().IsRegular() { return Document{}, errors.New("source is not a regular file") }
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
	b, err := readFileBounded(path,MaxFileBytes)
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
	b, err := readFileBounded(path,MaxFileBytes)
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

func readFileBounded(path string,limit int64)([]byte,error){
	file,err:=source.Open(path);if err!=nil{return nil,err};defer file.Close();data,err:=io.ReadAll(io.LimitReader(file,limit+1));if err!=nil{return nil,err};if int64(len(data))>limit{return nil,fmt.Errorf("file exceeds %d-byte limit",limit)};return data,nil
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

func normalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	for i := range lines { lines[i] = strings.TrimRight(lines[i], " \t") }
	return strings.TrimSuffix(strings.Join(lines, "\n"), "\n")
}
