package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zenodea/hyprland-mcp/internal/tools"
)

var version = "dev"

func init() {
	if info, ok := debug.ReadBuildInfo(); ok && version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
}

func main() {
	disable := flag.String("disable", "", "comma-separated tool groups to leave out ("+strings.Join(tools.Groups(false), ", ")+")")
	enable := flag.String("enable", "", "comma-separated opt-in tool groups to add ("+strings.Join(tools.Groups(true), ", ")+")")
	config := flag.String("config", tools.DefaultConfig(), "JSON file defining extra command tools")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "hyprland", Version: version}, tools.ServerOptions())
	tools.ConfigPath = *config
	if err := tools.Register(server, groupSet(*disable), groupSet(*enable)); err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	tools.Watch(ctx, server)

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}

func groupSet(list string) map[string]bool {
	set := map[string]bool{}
	for _, g := range strings.Split(list, ",") {
		if g = strings.TrimSpace(g); g != "" {
			set[g] = true
		}
	}
	return set
}
