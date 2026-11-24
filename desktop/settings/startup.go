package settings

type Registration interface {
	IsEnabled() (bool, error)
	Enable() error
	Disable() error
}

type Startup struct {
	Manager Registration
}

func (s *Startup) Enabled() (bool, error) {
	return s.Manager.IsEnabled()
}

func (s *Startup) SetEnabled(enabled bool) (bool, error) {
	var err error
	if enabled {
		err = s.Manager.Enable()
	} else {
		err = s.Manager.Disable()
	}
	if err != nil {
		return false, err
	}
	return s.Enabled()
}
