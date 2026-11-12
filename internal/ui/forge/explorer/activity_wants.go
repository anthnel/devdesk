package explorer

// WantsBlink reports whether the footer load line needs the router's blink
// frames. It follows what is displayed: the router must arm its chain only
// when status() carries the activity dot, never for a state that merely has
// loading or refreshing flags set while another line (signed out, clone,
// selection, error) masks the load line. status() is a pure function of the
// model, so calling it here has no side effect. The rows' own runs are already
// asked for by the jobs registry (BlinkingView).
func (m Model) WantsBlink() bool { return m.status().Activity }
