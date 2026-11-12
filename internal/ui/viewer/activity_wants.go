package viewer

// WantsBlink reports whether the footer needs the router's blink frames. It
// follows what is displayed: a follow read shows no dot, so it asks for none.
// status() is a pure function of the model, so calling it has no side effect.
func (m Model) WantsBlink() bool { return m.status().Activity }
