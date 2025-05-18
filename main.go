package main

import (
	"fmt"
	"os"

	"github.com/xdevplatform/xurl/auth"
	"github.com/xdevplatform/xurl/cli"
	"github.com/xdevplatform/xurl/config"
)

func main() {
	// Create a new config from environment variables
	config := config.NewConfig()
	auth := auth.NewAuth(config)

	// Create the root command
	rootCmd := cli.CreateRootCommand(config, auth)

	// Execute the command
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
