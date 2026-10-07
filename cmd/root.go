// Package cmd wires up the clockify CLI commands (Cobra + Viper + Huh + Lip Gloss).
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/AdongoJr2/clockify-cli/pkg/clockify"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// version is injected at build time:
//
//	go build -ldflags "-X github.com/AdongoJr2/clockify-cli/cmd.version=v1.0.0"
var version = "dev"

// cfgFile holds the optional --config flag value.
var cfgFile string

var rootCmd = &cobra.Command{
	Use:   "clockify",
	Short: "Colorful Clockify time tracking from your terminal",
	Long: banner() + `

clockify lets you start and stop timers, log past time entries, and list
your recent activity — without leaving the terminal.`,
	Version:       version,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command and exits non-zero on failure.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		printError(err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default $XDG_CONFIG_HOME/clockify-cli/config.yaml)")
	rootCmd.AddCommand(configCmd, logCmd, startCmd, stopCmd, listCmd, updateCmd, deleteCmd, projectsCmd, duplicateCmd)
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		return initConfig()
	}
}

// initConfig loads the YAML config file (if present) and binds CLOCKIFY_*
// environment variables, which take precedence over the file.
func initConfig() error {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
		if dir, err := configDir(); err == nil {
			viper.AddConfigPath(dir)
		}
	}

	viper.SetEnvPrefix("CLOCKIFY")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()
	viper.SetDefault("base_url", clockify.DefaultBaseURL)

	if err := viper.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if errors.As(err, &notFound) || errors.Is(err, fs.ErrNotExist) {
			return nil // first run: credentials may still come from env vars
		}
		return fmt.Errorf("read config: %w", err)
	}
	return nil
}

// configDir returns ~/.config/clockify-cli (honoring $XDG_CONFIG_HOME).
func configDir() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "clockify-cli"), nil
}

// configFilePath is the full path of the config file written by `config init`.
func configFilePath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// newClient builds an API client from the configured credentials.
func newClient() (*clockify.Client, error) {
	apiKey := strings.TrimSpace(viper.GetString("api_key"))
	if apiKey == "" {
		return nil, errors.New("API key not configured — run `clockify config init` or set CLOCKIFY_API_KEY")
	}
	return clockify.NewClient(viper.GetString("base_url"), apiKey), nil
}

// requireWorkspaceID returns the configured workspace, or a helpful error.
func requireWorkspaceID() (string, error) {
	ws := strings.TrimSpace(viper.GetString("workspace_id"))
	if ws == "" {
		return "", errors.New("workspace not configured — run `clockify config init` or set CLOCKIFY_WORKSPACE_ID")
	}
	return ws, nil
}

// currentUser resolves the authenticated user (cached on disk for 24h).
func currentUser(ctx context.Context, client *clockify.Client) (*clockify.User, error) {
	return getCachedUser(ctx, client, viper.GetString("api_key"))
}
