package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func completionCommand(root *cobra.Command, out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion",
		Short: "Generate shell completion scripts",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "bash",
		Short: "Generate bash completions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return root.GenBashCompletion(out)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "zsh",
		Short: "Generate zsh completions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return root.GenZshCompletion(out)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "fish",
		Short: "Generate fish completions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return root.GenFishCompletion(out, true)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "powershell",
		Short: "Generate PowerShell completions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return root.GenPowerShellCompletion(out)
		},
	})
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("choose a shell: bash, zsh, fish, or powershell")
	}

	return cmd
}
