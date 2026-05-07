package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/urfave/cli/v2"
	"go.uber.org/zap"

	"github.com/cupogo/andvari/utils/zlog"

	api "github.com/liut/morign/pkg/web/api"

	"github.com/liut/morign/htdocs"
	"github.com/liut/morign/pkg/services/llm"
	"github.com/liut/morign/pkg/services/stores"
	"github.com/liut/morign/pkg/services/tools"
	"github.com/liut/morign/pkg/settings"
	"github.com/liut/morign/pkg/web"
)

func usage(cc *cli.Context) error {
	return settings.Usage()
}

func initdb(cc *cli.Context) error {
	return stores.InitDB(cc.Context)
}

func importDocs(cc *cli.Context) error {
	input := cc.Args().First()
	file, err := os.Open(input)
	if err != nil {
		logger().Warnw("open fail", "input", input, "err", err)
		return err
	}
	defer file.Close()
	difflog := cc.String("diff")
	var lw *os.File // log write dat
	if len(difflog) > 0 {
		lw, err = os.Create(difflog)
		if err != nil {
			logger().Warnw("create fail", "difflog", difflog, "err", err)
			return err
		}
		defer lw.Close()
	} else {
		lw = os.Stderr
	}
	err = stores.Sgt().Corpus().ImportDocs(cc.Context, file, lw)
	if err != nil {
		logger().Warnw("import fail", "input", input, "err", err)
		return err
	}
	return nil
}

func importSwagger(cc *cli.Context) error {
	input := cc.Args().First()
	if input == "" {
		return fmt.Errorf("input file or directory is required")
	}

	file, err := os.Open(input)
	if err != nil {
		logger().Warnw("open fail", "input", input, "err", err)
		return err
	}
	defer file.Close()

	difflog := cc.String("diff")
	var lw *os.File
	if len(difflog) > 0 {
		lw, err = os.Create(difflog)
		if err != nil {
			logger().Warnw("create fail", "difflog", difflog, "err", err)
			return err
		}
		defer lw.Close()
	} else {
		lw = os.Stderr
	}

	err = stores.Sgt().Capability().ImportCapabilities(cc.Context, file, lw)
	if err != nil {
		logger().Warnw("import swagger fail", "input", input, "err", err)
		return err
	}
	return nil
}

func exportDocs(cc *cli.Context) error {
	output := cc.Args().First() // csv
	file, err := os.OpenFile(output, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		logger().Warnw("open fail", "output", output, "err", err)
		return err
	}
	defer file.Close()
	ctx := context.Background()
	spec := &stores.CobDocumentSpec{}
	spec.Limit = 90
	spec.Sort = "id"
	ea := stores.ExportArg{
		Spec:   spec,
		Out:    file,
		Format: cc.String("format"),
	}
	return stores.Sgt().Corpus().ExportDocs(ctx, ea)
}

func embeddingDocVector(cc *cli.Context) error {
	ctx := context.Background()
	target := cc.String("target")

	switch target {
	case "doc":
		spec := &stores.CobDocumentSpec{}
		spec.Limit = cc.Int("limit")
		spec.Sort = "id"
		return stores.Sgt().Corpus().SyncEmbeddingDocments(ctx, spec)
	case "mem":
		spec := &stores.ConvoMemorySpec{}
		spec.Limit = cc.Int("limit")
		spec.Sort = "id"
		return stores.Sgt().Convo().SyncEmbeddingMemories(ctx, spec)
	case "capability":
		spec := &stores.CapCapabilitySpec{}
		spec.Limit = cc.Int("limit")
		spec.Sort = "id"
		return stores.Sgt().Capability().SyncEmbeddingCapabilities(ctx, spec)
	default:
		return fmt.Errorf("unsupported target: %s (supported: doc, mem, capability)", target)
	}
}

func agent(cc *cli.Context) error {
	message := cc.String("message")
	stream := cc.Bool("stream")
	verbose := cc.Bool("verbose")
	interactive := cc.Bool("interactive")

	if !verbose {
		cfg := zap.NewProductionConfig()
		cfg.Level = zap.NewAtomicLevelAt(zap.ErrorLevel)
		zlogger, _ := cfg.Build()
		zlog.Set(zlogger.Sugar())
	}

	if !interactive && message == "" {
		return fmt.Errorf("message is required, use -m flag")
	}

	if err := stores.InitDB(cc.Context); err != nil {
		return err
	}

	client, err := stores.NewLLMClient(&settings.Current.Interact)
	if err != nil {
		return err
	}

	preset, _ := stores.LoadPreset()
	toolreg := tools.NewRegistry(stores.Sgt(),
		tools.WithClientInfo(settings.Current.Name, settings.Version()),
	)
	toolreg.ApplyToolDescriptions(preset.Tools)

	if err := toolreg.LoadServers(cc.Context, stores.Sgt()); err != nil {
		logger().Warnw("load MCP servers fail", "err", err)
	}

	ag := api.NewAgent(client, toolreg, preset.SystemPrompt, preset.ToolsPrompt)

	if interactive {
		return runInteractive(cc.Context, ag, stream)
	}

	ctx := cc.Context
	sysMsg, tools := ag.BuildSystemMessage(ctx)
	messages := []llm.Message{sysMsg, {Role: llm.RoleUser, Content: message}}

	if stream {
		cb := api.StreamCallbacks{
			OnDelta: func(delta string) {
				fmt.Print(delta)
			},
		}
		answer, err := ag.StreamChat(ctx, messages, tools, cb)
		if err != nil {
			return fmt.Errorf("stream chat: %w", err)
		}
		_ = answer
		fmt.Println()
	} else {
		answer, err := ag.Chat(ctx, messages, tools)
		if err != nil {
			return fmt.Errorf("chat: %w", err)
		}
		fmt.Println(answer)
	}

	return nil
}

func runInteractive(ctx context.Context, ag *api.Agent, stream bool) error {
	scanner := bufio.NewScanner(os.Stdin)
	sysMsg, tools := ag.BuildSystemMessage(ctx)
	messages := []llm.Message{sysMsg}

	fmt.Println("Agent REPL. Type /exit to quit.")
	for {
		fmt.Fprint(os.Stdout, "> ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "/exit" {
			break
		}

		messages = append(messages, llm.Message{Role: llm.RoleUser, Content: input})

		if stream {
			cb := api.StreamCallbacks{
				OnDelta: func(delta string) {
					fmt.Print(delta)
				},
			}
			answer, err := ag.StreamChat(ctx, messages, tools, cb)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\nerror: %v\n", err)
				continue
			}
			fmt.Println()
			if answer != "" {
				messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: answer})
			}
		} else {
			answer, err := ag.Chat(ctx, messages, tools)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				continue
			}
			fmt.Println(answer)
			messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: answer})
		}
	}
	return scanner.Err()
}

func logger() zlog.Logger {
	return zlog.Get()
}

func main() {

	var zlogger *zap.Logger
	if settings.InDevelop() {
		zlogger, _ = zap.NewDevelopment()
	} else {
		zlogger, _ = zap.NewProduction()
	}
	sugar := zlogger.Sugar()
	zlog.Set(sugar)

	app := &cli.App{
		Usage:                  "A Backend for OpenAI/ChatGPT",
		UseShortOptionHandling: true,
		Commands: []*cli.Command{
			{
				Name:    "usage",
				Aliases: []string{"env"},
				Usage:   "show usage",
				Action:  usage,
			},
			{
				Name:   "initdb",
				Usage:  "init database schema",
				Action: initdb,
			},
			{
				Name:   "import",
				Usage:  "import documents from a csv",
				Action: importDocs,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "diff", Aliases: []string{"diff-log"}, Value: "", Usage: "a filename of diff"},
				},
			},
			{
				Name:    "import-swagger",
				Usage:   "import API capabilities from swagger yaml/json",
				Aliases: []string{"import-capability"},
				Action:  importSwagger,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "diff", Aliases: []string{"diff-log"}, Value: "", Usage: "a filename of diff"},
				},
			},
			{
				Name:    "export",
				Usage:   "export documents to a csv",
				Aliases: []string{"exportDocs"},
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "format", Aliases: []string{"t"}, Value: "csv", Usage: "csv|jsonl"},
				},
				Action: exportDocs,
			},
			{
				Name:    "embedding",
				Usage:   "read prompt documents and embedding",
				Aliases: []string{"embedding-doc-vec"},
				Action:  embeddingDocVector,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "target", Aliases: []string{"t"}, Value: "doc", Usage: "target to embed: doc|mem|capability"},
					&cli.IntFlag{Name: "limit", Aliases: []string{"l"}, Value: 90, Usage: "limit for query"},
				},
			},
			{
				Name:    "agent",
				Usage:   "test LLM agent",
				Aliases: []string{"llm", "chat"},
				Action:  agent,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "message", Aliases: []string{"m"}, Usage: "message to send"},
					&cli.BoolFlag{Name: "stream", Aliases: []string{"s"}, Usage: "enable streaming response"},
					&cli.BoolFlag{Name: "verbose", Aliases: []string{"v"}, Usage: "show logs"},
					&cli.BoolFlag{Name: "interactive", Aliases: []string{"i"}, Usage: "interactive REPL mode"},
				},
			},
			{
				Name:    "web",
				Aliases: []string{"run"},
				Usage:   "run a web server",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "listen", Aliases: []string{"l"}, Value: settings.Current.HTTPListen, Usage: "http listen address"},
				},
				Action: webRun,
			},
			{
				Name: "version", Aliases: []string{"ver"},
				Usage: "show build version",
				Action: func(ctx *cli.Context) error {
					sugar.Infow("", "version", settings.Version(), "runtime", runtime.Version())
					return nil
				},
			},
		},
	}
	// if len(os.Args) < 2 {
	// 	webRun()
	// 	return
	// }
	if err := app.Run(os.Args); err != nil {
		logger().Fatalw("app run fail", "err", err)
	}
}

func webRun(cc *cli.Context) error {
	if settings.InDevelop() {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}
	if err := stores.InitDB(cc.Context); err != nil {
		return err
	}

	srv := web.New(web.Config{
		Addr:       cc.String("listen"),
		Debug:      settings.InDevelop(),
		DocHandler: http.FileServer(http.FS(htdocs.FS())),
	})

	ctx := context.Background()
	go func() {
		quit := make(chan os.Signal, 2)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
		logger().Info("shuting down server...")
		if err := srv.Stop(ctx); err != nil {
			logger().Infow("server shutdown:", "err", err)
		}
	}()

	return srv.Serve(ctx)
}
