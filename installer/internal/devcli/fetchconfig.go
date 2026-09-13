package devcli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/Developer-Simon/energy-node-installer/internal/transport"
)

// FetchConfigArgs configures one call to RunFetchConfig.
type FetchConfigArgs struct {
	Client            *transport.Client
	RemoteConfigPath  string                            // e.g. /etc/energy-node/config.json
	LocalTemplatePath string                            // e.g. services/energy-node.config.json
	Confirm           func(prompt string) (bool, error) // nil uses a real terminal prompt
	Stdout            io.Writer
}

// RunFetchConfig downloads the node's config.json into the repository
// template, asking first if that template already exists -- see this task's
// rationale for why declining must leave the existing file untouched.
func RunFetchConfig(ctx context.Context, args FetchConfigArgs) error {
	tmp, err := os.CreateTemp("", "energy-node-config-*.json")
	if err != nil {
		return fmt.Errorf("creating a temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	if err := args.Client.DownloadFile(args.RemoteConfigPath, tmpPath); err != nil {
		return fmt.Errorf("downloading %s: %w", args.RemoteConfigPath, err)
	}

	if _, err := os.Stat(args.LocalTemplatePath); err == nil {
		confirm := args.Confirm
		if confirm == nil {
			confirm = defaultConfirm
		}
		ok, err := confirm(fmt.Sprintf("%s already exists. Overwrite it with the node's current config.json?", args.LocalTemplatePath))
		if err != nil {
			return fmt.Errorf("confirming overwrite: %w", err)
		}
		if !ok {
			return fmt.Errorf("aborted: %s was not overwritten", args.LocalTemplatePath)
		}
	}

	content, err := os.ReadFile(tmpPath)
	if err != nil {
		return fmt.Errorf("reading downloaded config.json: %w", err)
	}
	if err := os.WriteFile(args.LocalTemplatePath, content, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", args.LocalTemplatePath, err)
	}

	fmt.Fprintf(args.Stdout, "Wrote %s from %s.\nCheck it before committing: ./scripts/deploy/check_tracked_secrets.sh\n",
		args.LocalTemplatePath, args.RemoteConfigPath)
	return nil
}
