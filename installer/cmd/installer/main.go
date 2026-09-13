// cmd/installer is the developer-CLI entry point (E12): deploy, ensure-
// secrets, fetch-config and diagnose reuse the same internal/devcli
// orchestration a graphical installer would (Plan C), without Schicht 2-4.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Developer-Simon/energy-node-installer/internal/devcli"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "deploy":
		err = runDeployCmd(os.Args[2:])
	case "ensure-secrets":
		err = runEnsureSecretsCmd(os.Args[2:])
	case "fetch-config":
		err = runFetchConfigCmd(os.Args[2:])
	case "diagnose":
		err = runDiagnoseCmd(os.Args[2:])
	case "-h", "--help", "help":
		printUsage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprint(os.Stderr, `energy-node-installer developer CLI (E12)

Usage:
  installer deploy [--only <dashboard|wheels|<service>>] [--dry-run] [--force-config] [--dev-unsigned] [common flags]
  installer ensure-secrets [--dev-unsigned] [common flags]
  installer fetch-config [common flags]
  installer diagnose [common flags]

Running without a subcommand only prints this message: the graphical
installer (Plan C) is not part of this build yet.

Common flags:
  --repo <path>            repository checkout (default: current directory)
  --host/--user/--base     override deploy-target.env
  --target-env <path>      deploy-target.env location (default: <repo>/secrets/deploy-target.env)
  --identity <path>        SSH private key (default: ~/.ssh/id_ed25519 if present)
  --password-file <path>   cached SSH password (default: <repo>/secrets/system-ssh.pw)
`)
}

// commonFlags is shared by every subcommand's flag.FlagSet.
type commonFlags struct {
	repoRoot     string
	host         string
	user         string
	base         string
	targetEnv    string
	identity     string
	passwordFile string
	arch         string
	pythonMinor  string
	abi          string
	signKey      string
	devUnsigned  bool
}

func addCommonFlags(fs *flag.FlagSet, c *commonFlags, repoRootDefault string) {
	fs.StringVar(&c.repoRoot, "repo", repoRootDefault, "repository checkout to build a bundle from")
	fs.StringVar(&c.host, "host", "", "override TARGET_HOST from deploy-target.env")
	fs.StringVar(&c.user, "user", "", "override TARGET_USER from deploy-target.env")
	fs.StringVar(&c.base, "base", "", "override TARGET_BASE from deploy-target.env")
	fs.StringVar(&c.targetEnv, "target-env", "", "path to deploy-target.env (default: <repo>/secrets/deploy-target.env)")
	fs.StringVar(&c.identity, "identity", "", "SSH private key path (default: ~/.ssh/id_ed25519 if present)")
	fs.StringVar(&c.passwordFile, "password-file", "", "path to a cached SSH password (default: <repo>/secrets/system-ssh.pw)")
}

func defaultPasswordFilePath(repoRoot string) string {
	return filepath.Join(repoRoot, "secrets", "system-ssh.pw")
}

func resolveTarget(c commonFlags) (devcli.Target, error) {
	targetEnvPath := c.targetEnv
	if targetEnvPath == "" {
		targetEnvPath = devcli.DefaultDeployTargetPath(c.repoRoot)
	}
	target, err := devcli.LoadTarget(targetEnvPath)
	if err != nil {
		return devcli.Target{}, fmt.Errorf("loading target from %s: %w", targetEnvPath, err)
	}
	return target.Override(c.host, c.user, c.base), nil
}

func connectFromFlags(ctx context.Context, target devcli.Target, c commonFlags) (*transport.Client, error) {
	identity := c.identity
	if identity == "" {
		if p, ok := devcli.DefaultIdentityPath(); ok {
			identity = p
		}
	}
	passwordFile := c.passwordFile
	if passwordFile == "" {
		passwordFile = defaultPasswordFilePath(c.repoRoot)
	}
	knownHosts, err := devcli.DefaultKnownHostsPath()
	if err != nil {
		return nil, err
	}
	return devcli.Connect(ctx, target, devcli.ConnectOptions{
		IdentityPath:   identity,
		PasswordFile:   passwordFile,
		KnownHostsPath: knownHosts,
	})
}

// --- deploy -----------------------------------------------------------

type deployConfig struct {
	common      commonFlags
	only        string
	dryRun      bool
	forceConfig bool
}

func parseDeployFlags(args []string, repoRootDefault string) (deployConfig, error) {
	fs := flag.NewFlagSet("deploy", flag.ContinueOnError)
	var cfg deployConfig
	addCommonFlags(fs, &cfg.common, repoRootDefault)
	cfg.common.arch = "armv6"
	fs.StringVar(&cfg.common.arch, "arch", cfg.common.arch, "target node architecture (armv6, arm64, amd64)")
	fs.StringVar(&cfg.common.pythonMinor, "python-minor", "", "override the bundle's Python minor version")
	fs.StringVar(&cfg.common.abi, "abi", "", "override the bundle's wheel ABI tag")
	fs.StringVar(&cfg.common.signKey, "sign-key", "", "path to an ed25519 private key for signing (optional)")
	fs.BoolVar(&cfg.common.devUnsigned, "dev-unsigned", false, "skip bundle signature verification (local and remote); there is no private key for the embedded release public key outside CI, so this is required to deploy a local build at all")
	fs.StringVar(&cfg.only, "only", "", `deploy just "dashboard", "wheels", or a device service id`)
	fs.BoolVar(&cfg.dryRun, "dry-run", false, "preview changes without touching the node")
	fs.BoolVar(&cfg.forceConfig, "force-config", false, "overwrite the node's existing config.json (asks first)")
	if err := fs.Parse(args); err != nil {
		return deployConfig{}, err
	}
	return cfg, nil
}

func runDeployCmd(args []string) error {
	repoRootDefault, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, err := parseDeployFlags(args, repoRootDefault)
	if err != nil {
		return err
	}

	target, err := resolveTarget(cfg.common)
	if err != nil {
		return err
	}
	ctx := context.Background()
	client, err := connectFromFlags(ctx, target, cfg.common)
	if err != nil {
		return err
	}
	defer client.Close()

	return devcli.RunDeploy(ctx, devcli.DeployArgs{
		Target:      target,
		Client:      client,
		RepoRoot:    cfg.common.repoRoot,
		Arch:        cfg.common.arch,
		PythonMinor: cfg.common.pythonMinor,
		ABI:         cfg.common.abi,
		SignKeyPath: cfg.common.signKey,
		DevUnsigned: cfg.common.devUnsigned,
		Only:        cfg.only,
		DryRun:      cfg.dryRun,
		ForceConfig: cfg.forceConfig,
		Stdout:      os.Stdout,
	})
}

// --- ensure-secrets -----------------------------------------------------

type ensureSecretsConfig struct {
	common          commonFlags
	mqttSecretPath  string
	adminSecretPath string
}

func parseEnsureSecretsFlags(args []string, repoRootDefault string) (ensureSecretsConfig, error) {
	fs := flag.NewFlagSet("ensure-secrets", flag.ContinueOnError)
	var cfg ensureSecretsConfig
	addCommonFlags(fs, &cfg.common, repoRootDefault)
	cfg.common.arch = "armv6"
	fs.StringVar(&cfg.common.arch, "arch", cfg.common.arch, "target node architecture (armv6, arm64, amd64)")
	fs.StringVar(&cfg.common.pythonMinor, "python-minor", "", "override the bundle's Python minor version")
	fs.StringVar(&cfg.common.abi, "abi", "", "override the bundle's wheel ABI tag")
	fs.StringVar(&cfg.common.signKey, "sign-key", "", "path to an ed25519 private key for signing (optional)")
	fs.BoolVar(&cfg.common.devUnsigned, "dev-unsigned", false, "skip bundle signature verification (local and remote); there is no private key for the embedded release public key outside CI, so this is required to deploy a local build at all")
	fs.StringVar(&cfg.mqttSecretPath, "mqtt-secret-path", "", "local cache for the MQTT password (default: <repo>/secrets/mqtt.pw)")
	fs.StringVar(&cfg.adminSecretPath, "admin-secret-path", "", "local cache for the dashboard admin password (default: <repo>/secrets/dashboard-admin.pw)")
	if err := fs.Parse(args); err != nil {
		return ensureSecretsConfig{}, err
	}
	if cfg.mqttSecretPath == "" {
		cfg.mqttSecretPath = filepath.Join(cfg.common.repoRoot, "secrets", "mqtt.pw")
	}
	if cfg.adminSecretPath == "" {
		cfg.adminSecretPath = filepath.Join(cfg.common.repoRoot, "secrets", "dashboard-admin.pw")
	}
	return cfg, nil
}

func runEnsureSecretsCmd(args []string) error {
	repoRootDefault, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, err := parseEnsureSecretsFlags(args, repoRootDefault)
	if err != nil {
		return err
	}

	target, err := resolveTarget(cfg.common)
	if err != nil {
		return err
	}
	ctx := context.Background()
	client, err := connectFromFlags(ctx, target, cfg.common)
	if err != nil {
		return err
	}
	defer client.Close()

	return devcli.RunEnsureSecrets(ctx, devcli.EnsureSecretsArgs{
		Target:          target,
		Client:          client,
		RepoRoot:        cfg.common.repoRoot,
		Arch:            cfg.common.arch,
		PythonMinor:     cfg.common.pythonMinor,
		ABI:             cfg.common.abi,
		SignKeyPath:     cfg.common.signKey,
		DevUnsigned:     cfg.common.devUnsigned,
		MQTTSecretPath:  cfg.mqttSecretPath,
		AdminSecretPath: cfg.adminSecretPath,
		Stdout:          os.Stdout,
	})
}

// --- fetch-config -----------------------------------------------------

type fetchConfigConfig struct {
	common            commonFlags
	remoteConfigPath  string
	localTemplatePath string
}

func parseFetchConfigFlags(args []string, repoRootDefault string) (fetchConfigConfig, error) {
	fs := flag.NewFlagSet("fetch-config", flag.ContinueOnError)
	var cfg fetchConfigConfig
	addCommonFlags(fs, &cfg.common, repoRootDefault)
	fs.StringVar(&cfg.remoteConfigPath, "remote-path", "/etc/energy-node/config.json", "config.json path on the node")
	fs.StringVar(&cfg.localTemplatePath, "local", "", "local template path (default: <repo>/services/energy-node.config.json)")
	if err := fs.Parse(args); err != nil {
		return fetchConfigConfig{}, err
	}
	if cfg.localTemplatePath == "" {
		cfg.localTemplatePath = filepath.Join(cfg.common.repoRoot, "services", "energy-node.config.json")
	}
	return cfg, nil
}

func runFetchConfigCmd(args []string) error {
	repoRootDefault, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, err := parseFetchConfigFlags(args, repoRootDefault)
	if err != nil {
		return err
	}

	target, err := resolveTarget(cfg.common)
	if err != nil {
		return err
	}
	ctx := context.Background()
	client, err := connectFromFlags(ctx, target, cfg.common)
	if err != nil {
		return err
	}
	defer client.Close()

	return devcli.RunFetchConfig(ctx, devcli.FetchConfigArgs{
		Client:            client,
		RemoteConfigPath:  cfg.remoteConfigPath,
		LocalTemplatePath: cfg.localTemplatePath,
		Stdout:            os.Stdout,
	})
}

// --- diagnose -----------------------------------------------------------

func parseDiagnoseFlags(args []string, repoRootDefault string) (commonFlags, error) {
	fs := flag.NewFlagSet("diagnose", flag.ContinueOnError)
	var c commonFlags
	addCommonFlags(fs, &c, repoRootDefault)
	if err := fs.Parse(args); err != nil {
		return commonFlags{}, err
	}
	return c, nil
}

func runDiagnoseCmd(args []string) error {
	repoRootDefault, err := os.Getwd()
	if err != nil {
		return err
	}
	common, err := parseDiagnoseFlags(args, repoRootDefault)
	if err != nil {
		return err
	}

	target, err := resolveTarget(common)
	if err != nil {
		return err
	}
	ctx := context.Background()
	client, err := connectFromFlags(ctx, target, common)
	if err != nil {
		return err
	}
	defer client.Close()

	return devcli.RunDiagnose(ctx, devcli.DiagnoseArgs{
		Client:          client,
		RemoteBundleDir: devcli.DefaultRemoteBundleDir,
		RemoteStateDir:  devcli.DefaultRemoteStateDir,
		Stdout:          os.Stdout,
	})
}
