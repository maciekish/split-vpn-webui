package systemd

// MockManager is a test helper implementing ServiceManager.
type MockManager struct {
	WriteUnitFunc  func(unitName, content string) error
	RemoveUnitFunc func(unitName string) error
	StartFunc      func(unitName string) error
	StopFunc       func(unitName string) error
	RestartFunc    func(unitName string) error
	EnableFunc     func(unitName string) error
	DisableFunc    func(unitName string) error
	StatusFunc     func(unitName string) (string, error)
	WriteBootFunc  func() error
}

func (m *MockManager) WriteUnit(unitName, content string) error {
	if m != nil && m.WriteUnitFunc != nil {
		return m.WriteUnitFunc(unitName, content)
	}
	return nil
}

func (m *MockManager) RemoveUnit(unitName string) error {
	if m != nil && m.RemoveUnitFunc != nil {
		return m.RemoveUnitFunc(unitName)
	}
	return nil
}

func (m *MockManager) Start(unitName string) error {
	if m != nil && m.StartFunc != nil {
		return m.StartFunc(unitName)
	}
	return nil
}

func (m *MockManager) Stop(unitName string) error {
	if m != nil && m.StopFunc != nil {
		return m.StopFunc(unitName)
	}
	return nil
}

func (m *MockManager) Restart(unitName string) error {
	if m != nil && m.RestartFunc != nil {
		return m.RestartFunc(unitName)
	}
	return nil
}

func (m *MockManager) Enable(unitName string) error {
	if m != nil && m.EnableFunc != nil {
		return m.EnableFunc(unitName)
	}
	return nil
}

func (m *MockManager) Disable(unitName string) error {
	if m != nil && m.DisableFunc != nil {
		return m.DisableFunc(unitName)
	}
	return nil
}

func (m *MockManager) Status(unitName string) (string, error) {
	if m != nil && m.StatusFunc != nil {
		return m.StatusFunc(unitName)
	}
	return "", nil
}

func (m *MockManager) WriteBootHook() error {
	if m != nil && m.WriteBootFunc != nil {
		return m.WriteBootFunc()
	}
	return nil
}
