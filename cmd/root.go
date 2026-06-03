/*
Copyright © 2024 Mahmoud Mousa <m.mousa@hey.com>

Licensed under the GNU GPL License, Version 3.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
https://www.gnu.org/licenses/gpl-3.0.en.html

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package cmd

import (
	"context"
	"os"
	"path/filepath"

	"github.com/mightymoud/sidekick/cmd/certstatus"
	"github.com/mightymoud/sidekick/cmd/config"
	"github.com/mightymoud/sidekick/cmd/deploy"
	"github.com/mightymoud/sidekick/cmd/initialize"
	"github.com/mightymoud/sidekick/cmd/launch"
	"github.com/mightymoud/sidekick/cmd/preview"
	"github.com/mightymoud/sidekick/utils"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

var version = "dev"

var rootCmd = &cobra.Command{
	Use:     "sidekick",
	Version: version,
	Short:   "CLI to self-host all your apps on a single VPS without vendor locking",
	Long:    `With sidekick you can deploy any number of applications to a single VPS, connect multiple domains and much more.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		initConfig(cmd)
	},
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.SetVersionTemplate(`{{println .Version}}`)

	home, err := os.UserHomeDir()
	if err != nil {
		pterm.Error.Println(err)
		os.Exit(1)
	}
	defaultConfigPath := filepath.Join(home, ".config", "sidekick", "default.yaml")

	rootCmd.PersistentFlags().String("config", defaultConfigPath, "Path to sidekick config file")

	rootCmd.AddCommand(initialize.InitCmd)
	rootCmd.AddCommand(preview.PreviewCmd)
	rootCmd.AddCommand(deploy.DeployCmd)
	rootCmd.AddCommand(launch.LaunchCmd)
	rootCmd.AddCommand(config.ConfigCmd)
	rootCmd.AddCommand(certstatus.CertStatusCmd)
}

func initConfig(cmd *cobra.Command) {
	var config utils.SidekickConfig

	viper.BindEnv("config", "SIDEKICK_CONFIG")
	viper.BindPFlag("config", cmd.Flags().Lookup("config"))

	configPath := viper.GetString("config")
	content, err := os.ReadFile(configPath)

	if err != nil {
		if requireConfigFile(cmd) {
			pterm.Fatal.Println("Sidekick config not found - Run sidekick init")
		}
		config = utils.SidekickConfig{
			Version:        "1",
			CurrentContext: "",
			Contexts:       []utils.SidekickContext{},
			Servers:        []utils.SidekickServer{},
		}
	} else {
		err := yaml.Unmarshal(content, &config)
		if err != nil {
			pterm.Fatal.Sprintf("Error unmarshaling the config yaml file: %s", err)
		}
	}

	if config.Version != "1" && !shouldSkipConfigVersionCheck(cmd) {
		pterm.Fatal.Println("An older version of the config file found. Please run 'sidekick config migrate'.")
	}

	ctx := context.WithValue(cmd.Context(), "config", &config)
	cmd.SetContext(ctx)
}

func requireConfigFile(cmd *cobra.Command) bool {
	cmdName := cmd.Name()

	if cmdName == "init" || cmdName == "help" {
		return false
	}

	if parentCmd := cmd.Parent(); parentCmd != nil {
		if parentCmd.Name() == "config" && cmdName == "migrate" {
			return false
		}
	}

	return true
}

func shouldSkipConfigVersionCheck(cmd *cobra.Command) bool {
	cmdName := cmd.Name()

	if cmdName == "help" {
		return true
	}

	if parentCmd := cmd.Parent(); parentCmd != nil {
		if parentCmd.Name() == "config" && cmdName == "migrate" {
			return true
		}
	}

	return false
}
