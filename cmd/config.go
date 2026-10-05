package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AdongoJr2/clockify-cli/pkg/clockify"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage clockify CLI configuration",
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Interactively configure your API key and workspace",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runConfigInit(cmd.Context())
	},
}

func init() {
	configCmd.AddCommand(configInitCmd)
}

// runConfigInit validates credentials against the API and saves them to
// ~/.config/clockify-cli/config.yaml with 0600 permissions.
func runConfigInit(ctx context.Context) error {
	if !isInteractive() {
		return errors.New("`clockify config init` requires an interactive terminal — alternatively set CLOCKIFY_API_KEY and CLOCKIFY_WORKSPACE_ID")
	}

	// 1. API key (hidden input; empty keeps a key already provided via env).
	var apiKey string
	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("Clockify API key").
			Description("Clockify → Profile Settings → API. Leave empty to keep the key from CLOCKIFY_API_KEY.").
			EchoMode(huh.EchoModePassword).
			Validate(func(s string) error {
				if strings.TrimSpace(s) == "" && strings.TrimSpace(viper.GetString("api_key")) == "" {
					return errors.New("an API key is required")
				}
				return nil
			}).
			Value(&apiKey),
	))
	if err := form.Run(); err != nil {
		return fmt.Errorf("configuration cancelled: %w", err)
	}
	if strings.TrimSpace(apiKey) != "" {
		apiKey = strings.TrimSpace(apiKey)
	} else {
		apiKey = strings.TrimSpace(viper.GetString("api_key"))
	}

	// 2. Validate the key before saving anything.
	client := clockify.NewClient(viper.GetString("base_url"), apiKey)
	var user *clockify.User
	if err := runWithSpinner("Validating API key…", func() error {
		var err error
		user, err = client.GetUser(ctx)
		return err
	}); err != nil {
		return fmt.Errorf("API key validation failed: %w", err)
	}

	// 3. Pick a workspace (fall back to manual entry if the API call fails).
	workspaceID, err := pickWorkspace(ctx, client)
	if err != nil {
		return err
	}

	// 4. Persist (env vars still take precedence at runtime).
	path, err := configFilePath()
	if err != nil {
		return fmt.Errorf("locate config dir: %w", err)
	}
	viper.Set("api_key", apiKey)
	viper.Set("workspace_id", workspaceID)
	viper.Set("base_url", viper.GetString("base_url"))
	viper.SetConfigFile(path)
	viper.SetConfigType("yaml")

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := viper.WriteConfigAs(path); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure config file: %w", err)
	}

	printSuccess("Configuration saved to %s", path)
	fmt.Printf("%s Logged in as %s %s\n", mutedStyle.Render("→"), user.Name, mutedStyle.Render("("+user.Email+")"))
	return nil
}

// pickWorkspace lets the user select a workspace, falling back to manual ID
// entry when the workspace list cannot be fetched.
func pickWorkspace(ctx context.Context, client *clockify.Client) (string, error) {
	var workspaces []clockify.Workspace
	err := runWithSpinner("Loading workspaces…", func() error {
		var err error
		workspaces, err = client.GetWorkspaces(ctx)
		return err
	})
	if err == nil && len(workspaces) > 0 {
		options := make([]huh.Option[clockify.Workspace], 0, len(workspaces))
		for _, w := range workspaces {
			options = append(options, huh.NewOption(w.Name, w))
		}
		var selected clockify.Workspace
		form := huh.NewForm(huh.NewGroup(
			huh.NewSelect[clockify.Workspace]().
				Title("Workspace").
				Description("Time entries will be tracked in this workspace.").
				Options(options...).
				Value(&selected),
		))
		if err := form.Run(); err != nil {
			return "", fmt.Errorf("configuration cancelled: %w", err)
		}
		fmt.Printf("%s Workspace: %s\n", mutedStyle.Render("→"), selected.Name)
		return selected.ID, nil
	}

	var manual string
	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("Workspace ID").
			Description("Could not fetch your workspaces — open Clockify in a browser and paste the workspace ID from the URL.").
			Validate(nonEmpty("workspace ID")).
			Value(&manual),
	))
	if err := form.Run(); err != nil {
		return "", fmt.Errorf("configuration cancelled: %w", err)
	}
	return strings.TrimSpace(manual), nil
}
