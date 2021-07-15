package main

import (
	"fmt"
	"github.com/spf13/cobra"
)

const version = "1.0.1"

var (
	// Flags
	verbose bool

	rootCmd = &cobra.Command{
		Use:   "yester",
		Short: "A YAML based API test runner",
		Long: `Yester is a YAML based API test runner.
For full documentation, reference https://github.com/serdnad/yester`,
		Run: func(cmd *cobra.Command, args []string) {
			// if version flag set, print version and return
			printVersion, _ := cmd.PersistentFlags().GetBool("version")
			if printVersion {
				fmt.Printf("yester v%s\n", version)
				return
			}

			// parse other flags
			verbose, _ = cmd.PersistentFlags().GetBool("verbose")

			run()
		},
	}
)

func main() {
	rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().BoolP("version", "v", false, "print the current version of yester")
	rootCmd.PersistentFlags().BoolP("verbose", "V", false, "print more details about test results")
}