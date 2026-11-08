package secrets

import "fmt"

// Mode is the input guard reaction to a finding.
type Mode string

// Modes.
const (
	ModeMask  Mode = "mask"
	ModeBlock Mode = "block"
)

// ParseMode validates s.
func ParseMode(s string) (Mode, error) {
	switch Mode(s) {
	case ModeMask, ModeBlock:
		return Mode(s), nil
	}
	return "", fmt.Errorf("guard mode must be %q or %q, got %q", ModeMask, ModeBlock, s)
}

// Action is the overall input decision.
type Action string

// Actions.
const (
	ActionPass    Action = "pass"
	ActionMasked  Action = "masked"
	ActionBlocked Action = "blocked"
)

// Policy maps finding types to modes.
type Policy struct {
	Default   Mode
	Overrides map[Type]Mode
}

// ModeFor returns the mode applied to f. Derived findings always block.
func (p Policy) ModeFor(f Finding) Mode {
	if f.Derived {
		return ModeBlock
	}
	if m, ok := p.Overrides[f.Type]; ok {
		return m
	}
	if p.Default == "" {
		return ModeMask
	}
	return p.Default
}

// Decide returns blocked if any finding blocks, masked if there are findings
// and all of them mask, pass otherwise.
func (p Policy) Decide(fs []Finding) Action {
	if len(fs) == 0 {
		return ActionPass
	}
	for _, f := range fs {
		if p.ModeFor(f) == ModeBlock {
			return ActionBlocked
		}
	}
	return ActionMasked
}
