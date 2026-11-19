// Command mdcix is the CLI entrypoint for the local markdown proxy.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/asmseo/mdcix/internal/config"
	"github.com/asmseo/mdcix/internal/convert"
	"github.com/asmseo/mdcix/internal/fetch"
	"github.com/asmseo/mdcix/internal/mitm"
	"github.com/asmseo/mdcix/internal/pro"
	"github.com/asmseo/mdcix/internal/proxy"
	"github.com/asmseo/mdcix/internal/stats"
	"github.com/asmseo/mdcix/internal/tokens"
)

// Set via -ldflags at release time.
var (
	version = "0.1.0-dev"
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
		Short: "Local proxy that serves clean Markdown to AI agents (saves tokens)",
		Long: `mdcix is an open-core local HTTP/HTTPS proxy that prefers Accept: text/markdown,
converts HTML to clean Markdown when needed, and reports token savings.

The universal integration path is the proxy itself: point HTTP_PROXY/HTTPS_PROXY
at mdcix so Grok, Claude, ChatGPT, Gemini, Grok Bot, Cursor agents, and any tool
that respects proxy env vars receive Markdown instead of noisy HTML.`,
		SilenceUsage: true,
	}
	root.PersistentFlags().StringVar(&cfgPath, "config", "", "path to YAML config (optional)")
	root.PersistentFlags().StringVar(&cfg.Listen, "listen", cfg.Listen, "proxy listen address")
	root.PersistentFlags().StringVar(&cfg.Mode, "mode", cfg.Mode, "conversion mode: reader|agent")
	root.PersistentFlags().StringVar(&cfg.Encoding, "encoding", cfg.Encoding, "tiktoken encoding")
	root.PersistentFlags().BoolVar(&cfg.MITM, "mitm", cfg.MITM, "enable HTTPS MITM (requires trusting local CA)")
	root.PersistentFlags().StringVar(&cfg.CertDir, "cert-dir", cfg.CertDir, "directory for MITM CA")
	root.PersistentFlags().BoolVar(&cfg.Insecure, "insecure", cfg.Insecure, "skip upstream TLS verify")
	root.PersistentFlags().StringVar(&cfg.UserAgent, "user-agent", cfg.UserAgent, "User-Agent for upstream")

	root.AddCommand(serveCmd(&cfg, &cfgPath))
	root.AddCommand(fetchCmd(&cfg, &cfgPath))
	root.AddCommand(versionCmd())
	root.AddCommand(mcpStubCmd())
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

func serveCmd(cfg *config.Config, cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the local markdown proxy (default :8080)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadCfg(*cfgPath, cfg); err != nil {
				return err
			}
			_ = pro.Disabled{} // open-core hook present at runtime

			ctr, err := tokens.New(cfg.Encoding)
			if err != nil {
				return err
			}
			session := &stats.Session{}

			var mitmMgr *mitm.Manager
			if cfg.MITM {
				mitmMgr, err = mitm.New(cfg.CertDir)
				if err != nil {
					return fmt.Errorf("mitm: %w", err)
				}
				log.Printf("MITM CA: %s (trust this cert for HTTPS conversion)", mitmMgr.CAPath())
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
}

func fetchCmd(cfg *config.Config, cfgPath *string) *cobra.Command {
	var showHeaders bool
	cmd := &cobra.Command{
		Use:   "fetch <url>",
		Short: "One-shot fetch URL as Markdown (for agents/CLI tools)",
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
	cmd.Flags().BoolVar(&showHeaders, "stats", true, "print token comparison (original vs markdown) to stderr")
	return cmd
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

func mcpStubCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "MCP/stdio bridge (stub — planned; use HTTP_PROXY for now)",
		Long: `MCP (Model Context Protocol) stdio bridge is planned for a later release.

Until then, the universal integration path is the HTTP proxy:

  mdcix serve
  export HTTP_PROXY=http://127.0.0.1:8080
  export HTTPS_PROXY=http://127.0.0.1:8080

Or call: mdcix fetch <url>

See README — section "MCP (planejado)".`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("mcp stdio bridge not implemented in MVP; use 'mdcix serve' + HTTP_PROXY/HTTPS_PROXY (universal) or 'mdcix fetch'")
		},
	}
}
