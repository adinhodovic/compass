package main

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	SetVersionInfo(version, commit, buildTime)
	Execute()
}
