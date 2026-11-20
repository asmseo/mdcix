// Command mdcix is the CLI entrypoint for budget-aware web retrieval.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/asmseo/mdcix/internal/cache"
	"github.com/asmseo/mdcix/internal/config"
	"github.com/asmseo/mdcix/internal/context/budget"
	"github.com/asmseo/mdcix/internal/convert"
	"github.com/asmseo/mdcix/internal/fetch"
	"github.com/asmseo/mdcix/internal/gateway"
	"github.com/asmseo/mdcix/internal/mitm"
	"github.com/asmseo/mdcix/internal/pro"
	"github.com/asmseo/mdcix/internal/proxy"
	"github.com/asmseo/mdcix/internal/stats"
	"github.com/asmseo/mdcix/internal/tokens"
	httpapi "github.com/asmseo/mdcix/internal/transport/http"
	"github.com/asmseo/mdcix/internal/transport/mcp"
)

// Set via -ldflags at release time.
var (
	version = "0.2.0-dev"
	commit  = "none"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var cfgPath string
	cfg := config.Default()

	root := &cobra.Command{
		Use:   "mdcix",
		Short: "Budget-aware web retrieval for AI agents (Markdown + sections + context)",
		Long: `mdcix is open-core, local-first infrastructure for budget-aware web consumption.

Markdown is the initial representation — the product is selective, measurable
retrieval: full page, outline, multi-section, or query+budget packing.

Universal integration: HTTP_PROXY/HTTPS_PROXY → mdcix serve, or CLI/REST/MCP tools.`,
		SilenceUsage: true,
	}
	root.PersistentFlags().StringVar(&cfgPath, "config", "", "path to YAML config (optional)")
	root.PersistentFlags().StringVar(&cfg.Listen, "listen", cfg.Listen, "proxy/API listen address")
	root.PersistentFlags().StringVar(&cfg.Mode, "mode", cfg.Mode, "conversion mode: reader|agent")
	root.PersistentFlags().StringVar(&cfg.Encoding, "encoding", cfg.Encoding, "tiktoken encoding")
	root.PersistentFlags().BoolVar(&cfg.MITM, "mitm", cfg.MITM, "enable HTTPS MITM (requires trusting local CA)")
	root.PersistentFlags().StringVar(&cfg.CertDir, "cert-dir", cfg.CertDir, "directory for MITM CA")
	root.PersistentFlags().BoolVar(&cfg.Insecure, "insecure", cfg.Insecure, "skip upstream TLS verify")
	root.PersistentFlags().StringVar(&cfg.UserAgent, "user-agent", cfg.UserAgent, "User-Agent for upstream")
	root.PersistentFlags().StringVar(&cfg.CacheDir, "cache-dir", cfg.CacheDir, "SQLite cache dir (or .db path); empty disables")

	root.AddCommand(serveCmd(&cfg, &cfgPath))
	root.AddCommand(fetchCmd(&cfg, &cfgPath))
	root.AddCommand(outlineCmd(&cfg, &cfgPath))
	root.AddCommand(readCmd(&cfg, &cfgPath))
	root.AddCommand(cacheCmd(&cfg, &cfgPath))
	root.AddCommand(versionCmd())
	root.AddCommand(mcpCmd(&cfg, &cfgPath))
	return root
}

func loadCfg(path string, cfg *config.Config) error {
	merged, err := config.LoadYAML(path, *cfg)
	if err != nil {
		return err
	}
	*cfg = merged
	return nil
}

func newEngine(cfg *config.Config) (*gateway.Engine, error) {
	ctr, err := tokens.New(cfg.Encoding)
	if err != nil {
		return nil, err
	}
	eng := &gateway.Engine{
		Counter:   ctr,
		Mode:      convert.Mode(cfg.Mode),
		UserAgent: cfg.UserAgent,
		Timeout:   cfg.Timeout,
		MaxBody:   cfg.MaxBody,
		Encoding:  cfg.Encoding,
	}
	if cfg.CacheDir != "" {
		store, err := cache.Open(cache.DBPath(cfg.CacheDir), cfg.CacheTTL)
		if err != nil {
			return nil, fmt.Errorf("cache: %w", err)
		}
		eng.Cache = store
	}
	return eng, nil
}

func cacheCmd(cfg *config.Config, cfgPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Inspect or clear the local SQLite document cache",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "stats",
		Short: "Print cache entry count",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadCfg(*cfgPath, cfg); err != nil {
				return err
			}
			if cfg.CacheDir == "" {
				return fmt.Errorf("cache disabled; set --cache-dir")
			}
			store, err := cache.Open(cache.DBPath(cfg.CacheDir), cfg.CacheTTL)
			if err != nil {
				return err
			}
			defer store.Close()
			n, err := store.Stats()
			if err != nil {
				return err
			}
			fmt.Printf("cache_path=%s entries=%d\n", cache.DBPath(cfg.CacheDir), n)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "clear",
		Short: "Delete all cached documents",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadCfg(*cfgPath, cfg); err != nil {
				return err
			}
			if cfg.CacheDir == "" {
				return fmt.Errorf("cache disabled; set --cache-dir")
			}
			store, err := cache.Open(cache.DBPath(cfg.CacheDir), cfg.CacheTTL)
			if err != nil {
				return err
			}
			defer store.Close()
			if err := store.Clear(); err != nil {
				return err
			}
			fmt.Println("cache cleared:", cache.DBPath(cfg.CacheDir))
			return nil
		},
	})
	return cmd
}

func serveCmd(cfg *config.Config, cfgPath *string) *cobra.Command {
	var api bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run local proxy (and optional REST API) on :8080",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadCfg(*cfgPath, cfg); err != nil {
				return err
			}
			_ = pro.Disabled{}

			ctr, err := tokens.New(cfg.Encoding)
			if err != nil {
				return err
			}
			session := &stats.Session{}
			eng, err := newEngine(cfg)
			if err != nil {
				return err
			}

			var mitmMgr *mitm.Manager
			if cfg.MITM {
				mitmMgr, err = mitm.New(cfg.CertDir)
				if err != nil {
					return fmt.Errorf("mitm: %w", err)
				}
				log.Printf("MITM CA: %s (trust this cert for HTTPS conversion)", mitmMgr.CAPath())
			}

			if api {
				srv := httpapi.New(eng, cfg.Listen)
				ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
				defer stop()
				go func() {
					<-ctx.Done()
					_ = srv.Close()
				}()
				log.Printf("mdcix REST API on %s (/v1/outline|/v1/read|/v1/sections|/v1/read-budget|/health)", cfg.Listen)
				return srv.ListenAndServe()
			}

			srv := proxy.New(proxy.Options{
				Addr:      cfg.Listen,
				Mode:      convert.Mode(cfg.Mode),
				MaxBody:   cfg.MaxBody,
				Insecure:  cfg.Insecure,
				UserAgent: cfg.UserAgent,
				Counter:   ctr,
				Session:   session,
				MITM:      mitmMgr,
				Timeout:   cfg.Timeout,
			})

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			go func() {
				<-ctx.Done()
				snap := session.Snapshot()
				log.Printf("shutdown — requests=%d saved_tokens=%d (%.1f%%)", snap.Requests, snap.Saved, session.Percent())
				_ = srv.Close()
			}()

			log.Printf("Point agents at this proxy:\n  export HTTP_PROXY=http://127.0.0.1%s\n  export HTTPS_PROXY=http://127.0.0.1%s", cfg.Listen, cfg.Listen)
			return srv.ListenAndServe()
		},
	}
	cmd.Flags().BoolVar(&api, "api", false, "serve REST API instead of forward proxy")
	return cmd
}

func fetchCmd(cfg *config.Config, cfgPath *string) *cobra.Command {
	var showHeaders bool
	cmd := &cobra.Command{
		Use:   "fetch <url>",
		Short: "One-shot fetch URL as Markdown",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadCfg(*cfgPath, cfg); err != nil {
				return err
			}
			ctr, err := tokens.New(cfg.Encoding)
			if err != nil {
				return err
			}
			res, err := fetch.Get(cmd.Context(), args[0], fetch.Options{
				Mode:      convert.Mode(cfg.Mode),
				UserAgent: cfg.UserAgent,
				Timeout:   cfg.Timeout,
				MaxBody:   cfg.MaxBody,
				Counter:   ctr,
			})
			if err != nil {
				return err
			}
			if showHeaders {
				fmt.Fprintf(os.Stderr, "--- mdcix token comparison ---\n")
				fmt.Fprintf(os.Stderr, "%s: %d\n", fetch.HeaderTokensOriginal, res.Savings.Original)
				fmt.Fprintf(os.Stderr, "%s: %d\n", fetch.HeaderTokensMarkdown, res.Savings.Markdown)
				fmt.Fprintf(os.Stderr, "%s: %d\n", fetch.HeaderTokensSaved, res.Savings.Saved)
				fmt.Fprintf(os.Stderr, "%s: %.1f\n", fetch.HeaderSavedPercent, res.Savings.Percent)
				fmt.Fprintf(os.Stderr, "%s: %s\n", fetch.HeaderSource, res.Savings.Source)
				fmt.Fprintf(os.Stderr, "--------------------------------\n")
			}
			fmt.Println(res.Markdown)
			return nil
		},
	}
	cmd.Flags().BoolVar(&showHeaders, "stats", true, "print token comparison to stderr")
	return cmd
}

func outlineCmd(cfg *config.Config, cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "outline <url>",
		Short: "Return compact page outline (section ids, titles, tokens)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadCfg(*cfgPath, cfg); err != nil {
				return err
			}
			eng, err := newEngine(cfg)
			if err != nil {
				return err
			}
			out, err := eng.Outline(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		},
	}
}

func readCmd(cfg *config.Config, cfgPath *string) *cobra.Command {
	var sections string
	var query string
	var tokBudget int
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "read <url>",
		Short: "Read full page, selected sections, or budget-ranked content",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadCfg(*cfgPath, cfg); err != nil {
				return err
			}
			eng, err := newEngine(cfg)
			if err != nil {
				return err
			}
			url := args[0]
			switch {
			case sections != "":
				ids := splitCSV(sections)
				res, err := eng.ReadSections(cmd.Context(), url, ids, tokBudget)
				if err != nil {
					return err
				}
				return printBudget(res, asJSON)
			case query != "":
				res, err := eng.ReadBudget(cmd.Context(), url, query, tokBudget)
				if err != nil {
					return err
				}
				return printBudget(res, asJSON)
			default:
				doc, err := eng.ReadPage(cmd.Context(), url)
				if err != nil {
					return err
				}
				if asJSON {
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					return enc.Encode(doc)
				}
				fmt.Println(doc.Markdown)
				fmt.Fprintf(os.Stderr, "document_version=%s sections=%d markdown_tokens=%d original_tokens=%d\n",
					doc.Version, len(doc.Sections), doc.MarkdownTokens, doc.OriginalTokens)
				return nil
			}
		},
	}
	cmd.Flags().StringVar(&sections, "sections", "", "comma-separated section ids")
	cmd.Flags().StringVar(&query, "query", "", "budget-aware lexical query (BM25)")
	cmd.Flags().IntVar(&tokBudget, "budget", 1500, "token budget for --sections/--query")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of Markdown")
	return cmd
}

func printBudget(res budget.Response, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	fmt.Println(res.Markdown)
	fmt.Fprintf(os.Stderr, "returned_tokens=%d budget=%d selected=%v truncated=%v warnings=%v\n",
		res.ReturnedTokens, res.RequestedBudget, res.Selected, res.Truncated, res.Warnings)
	return nil
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("mdcix %s (commit %s)\n", version, commit)
		},
	}
}

func mcpCmd(cfg *config.Config, cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "MCP stdio server (tools: page_outline, read_page, read_sections, read_budget)",
		Long: `MCP tools share the same gateway engine as CLI/REST.

Use outline when sections are unknown; multi-section when known; budget for targeted queries;
full page for short docs. Prefer agent-selected mode for cross-language tasks.

Configure Cursor / Claude Desktop with:
  {"command":"mdcix","args":["mcp"]}`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadCfg(*cfgPath, cfg); err != nil {
				return err
			}
			eng, err := newEngine(cfg)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			srv := mcp.New(eng, "mdcix", version)
			log.Printf("mdcix MCP stdio ready (page_outline|read_page|read_sections|read_budget)")
			return srv.Run(ctx)
		},
	}
}
