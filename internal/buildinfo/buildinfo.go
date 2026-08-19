package buildinfo

import "fmt"

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"buildDate"`
	GoVersion string `json:"goVersion"`
	Platform  string `json:"platform"`
}

func Current(goVersion, goos, goarch string) Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		BuildDate: BuildDate,
		GoVersion: goVersion,
		Platform:  goos + "/" + goarch,
	}
}

func (i Info) String() string {
	return fmt.Sprintf("version=%s commit=%s buildDate=%s go=%s platform=%s", i.Version, i.Commit, i.BuildDate, i.GoVersion, i.Platform)
}
