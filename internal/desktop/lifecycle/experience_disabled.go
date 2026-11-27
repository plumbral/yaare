//go:build !yaare_experience

package lifecycle

import "github.com/egoist/mygo"

func experiencePageOptions(session *mygo.Session) mygo.PageOptions {
	return mygo.PageOptions{Session: session}
}

func installExperienceProbe(*mygo.Window) {}
