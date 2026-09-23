package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zenodea/hyprland-mcp/internal/tools"
)

var version = "dev"

func main() {
	disable := flag.String("disable", "", "comma-separated tool groups to leave out ("+strings.Join(tools.Groups(), ", ")+")")
	config := flag.String("config", tools.DefaultConfig(), "JSON file defining extra command tools")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	skip := map[string]bool{}
	for _, g := range strings.Split(*disable, ",") {
		if g = strings.TrimSpace(g); g != "" {
			skip[g] = true
		}
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "hyprland", Version: version}, nil)
	tools.ConfigPath = *config
	if err := tools.Register(server, skip); err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}
