// Code generated by "stringer -type=state"; DO NOT EDIT.

package sessions

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[InMenu-0]
	_ = x[InInputDialog-1]
}

const _state_name = "InMenuInInputDialog"

var _state_index = [...]uint8{0, 6, 19}

func (i state) String() string {
	if i < 0 || i >= state(len(_state_index)-1) {
		return "state(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _state_name[_state_index[i]:_state_index[i+1]]
}
