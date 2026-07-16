//go:build mcpfetch
// +build mcpfetch

// MCP Fetch Server - 独立的网页抓取 MCP 服务
// 支持 STDIO 模式: go run scripts/mcp-fetch.go
// 支持 HTTP 模式:  go run scripts/mcp-fetch.go -http -port 3000
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	nurl "net/url"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	htmd "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/PuerkitoBio/goquery"
	readeck "codeberg.org/readeck/go-readability/v2"
	"golang.org/x/net/html/charset"
)

var (
	httpMode = flag.Bool("http", false, "Run in HTTP mode")
	httpPort = flag.Int("port", 3000, "HTTP server port")
)

const (
	userAgent   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	maxLength   = 5000
)

func main() {
	flag.Parse()

	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	srv := server.NewMCPServer(
		"mcp-fetch",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	srv.AddTool(mcp.NewTool("fetch",
		mcp.WithDescription("Fetch a website and return the content as Markdown. Supports max_length and start_index for pagination."),
		mcp.WithString("url",
			mcp.Description("URL of the website to fetch"),
			mcp.Required(),
		),
		mcp.WithNumber("max_length",
			mcp.Description("Maximum number of characters to return (default: 5000, max: 999999)"),
		),
		mcp.WithNumber("start_index",
			mcp.Description("Start content from this character index (default: 0)"),
		),
		mcp.WithBoolean("raw",
			mcp.Description("If true, returns raw content instead of Markdown"),
		),
	), handleFetch)

	if *httpMode {
		addr := fmt.Sprintf(":%d", *httpPort)
		http.HandleFunc("/call", func(w http.ResponseWriter, r *http.Request) {
			var req mcp.CallToolRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			result, _ := handleFetch(r.Context(), req)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(result)
		})
		http.HandleFunc("/tools", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]any{
				{
					"name":        "fetch",
					"description": "Fetch a website and return the content as Markdown",
					"inputSchema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"url":        map[string]any{"type": "string", "description": "URL to fetch"},
							"max_length": map[string]any{"type": "number", "description": "Max chars to return"},
							"start_index": map[string]any{"type": "number", "description": "Start position"},
							"raw":        map[string]any{"type": "boolean", "description": "Return raw content"},
						},
						"required": []string{"url"},
					},
				},
			})
		})
		log.Printf("Starting MCP Fetch HTTP Server on %s...", addr)
		if err := http.ListenAndServe(addr, nil); err != nil {
			log.Printf("HTTP Server error: %v", err)
		}
	} else {
		log.Println("Starting MCP Fetch Server (STDIO)...")
		if err := server.ServeStdio(srv); err != nil {
			log.Printf("Server error: %v", err)
		}
	}
}

func handleFetch(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := request.GetArguments()
	if args == nil {
		args = make(map[string]any)
	}

	urlStr := getString(args, "url")
	if urlStr == "" {
		return errorResult("missing required argument: url"), nil
	}

	maxLen := int(getFloat64(args, "max_length", float64(maxLength)))
	if maxLen <= 0 || maxLen >= 1000000 {
		return errorResult("max_length must be between 1 and 999999"), nil
	}

	startIndex := int(getFloat64(args, "start_index", 0))
	if startIndex < 0 {
		return errorResult("start_index must be >= 0"), nil
	}

	raw := getBool(args, "raw")

	content, err := fetchURL(ctx, urlStr, raw)
	if err != nil {
		return errorResult(err.Error()), nil
	}

	// Apply pagination
	origLen := len(content)
	if startIndex >= origLen {
		content = ""
	} else {
		end := min(startIndex+maxLen, origLen)
		content = content[startIndex:end]
		if len(content) == maxLen && origLen > end {
			content += fmt.Sprintf("\n\n[Content truncated. Call with start_index=%d to get more]", end)
		}
	}

	if content == "" {
		return errorResult("no content available"), nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewTextContent(content),
		},
	}, nil
}

func fetchURL(ctx context.Context, urlStr string, raw bool) (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	reader, _ := charset.NewReader(resp.Body, resp.Header.Get("Content-Type"))
	body, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}

	html := string(body)
	pagePreview := html
	if len(pagePreview) > 100 {
		pagePreview = pagePreview[:100]
	}

	isHTML := strings.Contains(resp.Header.Get("content-type"), "text/html") || strings.Contains(pagePreview, "<html>")

	if isHTML && !raw {
		return extractMarkdown(html, urlStr)
	}
	return html, nil
}

var converter = htmd.NewConverter("", true, nil)

func extractMarkdown(html, uri string) (string, error) {
	parsedURL, _ := nurl.Parse(uri)
	article, err := readeck.FromReader(strings.NewReader(html), parsedURL)
	if err != nil || article.Node == nil {
		return extractFallback(html)
	}

	var sb strings.Builder
	if err := article.RenderText(&sb); err != nil {
		return "", err
	}

	text := sb.String()
	if text == "" {
		return extractFallback(html)
	}

	md, err := converter.ConvertString(text)
	if err != nil {
		return text, nil
	}
	return md, nil
}

func extractFallback(html string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return strings.TrimSpace(html), nil
	}

	var sb strings.Builder
	doc.Find("h1, h2, h3, h4, h5, h6").Each(func(_ int, s *goquery.Selection) {
		tag := goquery.NodeName(s)
		level := 1
		switch tag {
		case "h1":
			level = 1
		case "h2":
			level = 2
		case "h3":
			level = 3
		case "h4", "h5", "h6":
			level = 4
		}
		sb.WriteString(strings.Repeat("#", level) + " " + strings.TrimSpace(s.Text()) + "\n\n")
	})

	doc.Find("p").Each(func(_ int, s *goquery.Selection) {
		sb.WriteString(strings.TrimSpace(s.Text()) + "\n\n")
	})

	doc.Find("ul li").Each(func(_ int, s *goquery.Selection) {
		sb.WriteString("- " + strings.TrimSpace(s.Text()) + "\n")
	})

	doc.Find("ol li").Each(func(i int, s *goquery.Selection) {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, strings.TrimSpace(s.Text())))
	})

	if sb.Len() == 0 {
		re := regexp.MustCompile(`(?s)<[^>]*>`)
		return strings.TrimSpace(re.ReplaceAllString(html, "")), nil
	}
	return sb.String(), nil
}

func getString(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

func getFloat64(args map[string]any, key string, defaultVal float64) float64 {
	if v, ok := args[key].(float64); ok {
		return v
	}
	return defaultVal
}

func getBool(args map[string]any, key string) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	return false
}

func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewTextContent(msg),
		},
		IsError: true,
	}
}
