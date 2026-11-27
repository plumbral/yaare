package app

import "runtime"

type Application struct{}

func New() *Application { return &Application{} }

var version = "0.1.0"

type Info struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	Platform     string `json:"platform"`
	Architecture string `json:"architecture"`
}

func (*Application) Info() Info {
	return Info{Name: "Yaare", Version: version, Platform: runtime.GOOS, Architecture: runtime.GOARCH}
}
