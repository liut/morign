package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	nurl "net/url"

	htmd "github.com/JohannesKaufmann/html-to-markdown"

	readeck "codeberg.org/readeck/go-readability/v2"
	"github.com/liut/morign/pkg/models/mcps"
)

const (
	DEFAULT_USER_AGENT_AUTONOMOUS = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

// invokeCurrentTime reports the local date, time and 时辰. The 时辰 line keeps
// the wording prompts carried before time moved out of the prompt.
func invokeCurrentTime(context.Context, map[string]any) (map[string]any, error) {
	now := time.Now()
	return mcps.BuildToolSuccessResult(fmt.Sprintf("%s\n当前时辰: %s",
		now.Format("2006-01-02 15:04:05 -0700 MST"), shichen(now.Hour()))), nil
}

// shichen maps an hour to its Chinese two-hour period.
func shichen(hour int) string {
	switch {
	case hour >= 23 || hour < 1:
		return "子时"
	case hour < 3:
		return "丑时"
	case hour < 5:
		return "寅时"
	case hour < 7:
		return "卯时"
	case hour < 9:
		return "辰时"
	case hour < 11:
		return "巳时"
	case hour < 13:
		return "午时"
	case hour < 15:
		return "未时"
	case hour < 17:
		return "申时"
	case hour < 19:
		return "酉时"
	case hour < 21:
		return "戌时"
	default:
		return "亥时"
	}
}

// Fetch implementation
func (r *Registry) callFetch(ctx context.Context, args map[string]any) (map[string]any, error) {
	var (
		urlStr     string
		maxLength  int
		startIndex int
		raw        bool
	)

	urlStr = mcps.StringArg(args, "url")
	if urlStr == "" {
		return mcps.BuildToolErrorResult("missing required argument: url"), nil
	}
	maxLength, _, _ = mcps.IntArg(args, "max_length")
	if maxLength == 0 {
		maxLength = 5000
	}
	startIndex, _, _ = mcps.IntArg(args, "start_index")
	raw, _, _ = mcps.BoolArg(args, "raw")

	// Input validation
	if maxLength <= 0 || maxLength >= 1000000 {
		return mcps.BuildToolErrorResult("max_length must be between 1 and 999999"), nil
	}
	if startIndex < 0 {
		return mcps.BuildToolErrorResult("start_index must be >= 0"), nil
	}

	// Fetch URL
	content, prefix, err := fetchURL(ctx, urlStr, DEFAULT_USER_AGENT_AUTONOMOUS, raw)
	if err != nil {
		logger().Info("fetch", "url", urlStr, "err", err)
		return mcps.BuildToolSuccessResult(err.Error()), nil
	}

	// Handle truncation
	originalLength := len(content)
	if startIndex >= originalLength {
		content = "<error>No more content available.</error>"
	} else {
		endIndex := min(startIndex+maxLength, originalLength)
		truncatedContent := content[startIndex:endIndex]
		if len(truncatedContent) == 0 {
			content = "<error>No more content available.</error>"
		} else {
			content = truncatedContent
			actualContentLength := len(truncatedContent)
			remainingContent := originalLength - (startIndex + actualContentLength)
			if actualContentLength == maxLength && remainingContent > 0 {
				nextStart := startIndex + actualContentLength
				content += fmt.Sprintf("\n\n<error>Content truncated. Call the fetch tool with a start_index of %d to get more content.</error>", nextStart)
			}
		}
	}
	logger().Debug("fetched", "url", urlStr, "content", content, "prefix", prefix)

	return mcps.BuildToolSuccessResult(fmt.Sprintf("%s\nContents of %s:\n%s", prefix, urlStr, content)), nil
}

var converter = htmd.NewConverter("", true, nil)

// extractContentFromHTML extracts text from HTML and converts to Markdown
func extractContentFromHTML(htmlContent, uri string) string {
	parsedURL, _ := nurl.Parse(uri)
	article, err := readeck.FromReader(strings.NewReader(htmlContent), parsedURL)
	if err != nil {
		return "<error>Page failed to be simplified from HTML</error>"
	}

	if article.Node == nil {
		return "<error>Page failed to be simplified from HTML</error>"
	}

	var sb strings.Builder
	if err := article.RenderText(&sb); err != nil {
		return "<error>Failed to render article text</error>"
	}

	textContent := sb.String()
	if textContent == "" {
		return "<error>Page failed to be simplified from HTML</error>"
	}

	markdown, err := converter.ConvertString(textContent)
	if err != nil {
		logger().Info("failed to convert HTML to markdown", "err", err)
		return "<error>Failed to convert HTML to markdown</error>"
	}

	return markdown
}

// fetchURL fetches web page content, supports HTML to Markdown conversion
func fetchURL(ctx context.Context, urlStr, userAgent string, raw bool) (content, prefix string, err error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		err = fmt.Errorf("HTTP %d", resp.StatusCode)
		return
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	content = string(b)

	contentType := resp.Header.Get("content-type")
	pagePreview := content
	if len(pagePreview) > 100 {
		pagePreview = pagePreview[:100]
	}
	isHTML := strings.Contains(contentType, "text/html") || strings.Contains(pagePreview, "<html")

	if isHTML && !raw {
		content = extractContentFromHTML(content, urlStr)
		prefix = "Markdown"
	} else {
		prefix = fmt.Sprintf("Content type %s, raw content:", contentType)
	}
	return
}
