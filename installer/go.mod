module github.com/Developer-Simon/energy-node-installer

go 1.27.1

require (
	github.com/Developer-Simon/energy-node-webui v0.0.0
	github.com/crgimenes/glaze v0.0.55-0.20260915115259-5b7e914573e6
	github.com/jchv/go-webview2 v0.0.0-20260205173254-56598839c808
	github.com/pkg/sftp v1.13.11
	github.com/zalando/go-keyring v0.2.8
	golang.org/x/crypto v0.57.0
	golang.org/x/term v0.46.0
)

require (
	github.com/danieljoos/wincred v1.2.3 // indirect
	github.com/ebitengine/purego v0.11.0 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/jchv/go-winloader v0.0.0-20250406163304-c1995be93bd1 // indirect
	github.com/kr/fs v0.1.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
)

replace github.com/Developer-Simon/energy-node-webui => ./webui
