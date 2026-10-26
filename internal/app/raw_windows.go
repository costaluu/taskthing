package app

import (
	"os"

	"golang.org/x/term"
)

type rawState struct{ state *term.State }

func makeRaw() (*rawState, error) {
	s, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return nil, err
	}
	return &rawState{s}, nil
}

func (s *rawState) restore() { term.Restore(int(os.Stdin.Fd()), s.state) }
