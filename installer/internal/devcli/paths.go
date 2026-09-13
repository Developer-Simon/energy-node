package devcli

// DefaultRemoteStateDir and DefaultRemoteBundleDir are the fixed node paths
// this CLI uses for every subcommand. The spec names
// "/var/lib/energy-node-installer/steps/<id>" and ".../selection.json"
// explicitly (Komponente A); this plan picks the same parent directory for
// the extracted bundle itself, so every piece of installer state on the node
// lives under one root.
const (
	DefaultRemoteStateDir  = "/var/lib/energy-node-installer"
	DefaultRemoteBundleDir = DefaultRemoteStateDir + "/bundle"
)
