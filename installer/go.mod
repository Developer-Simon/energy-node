module github.com/Developer-Simon/energy-node-installer

go 1.26.0

require (
	github.com/pkg/sftp v1.13.11
	golang.org/x/crypto v0.57.0
	golang.org/x/term v0.46.0
)

require (
	github.com/Developer-Simon/energy-node-webui v0.0.0
	github.com/kr/fs v0.1.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
)

replace github.com/Developer-Simon/energy-node-webui => ./webui
