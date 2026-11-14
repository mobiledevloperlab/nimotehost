// SPDX-License-Identifier: Apache-2.0
package host

import "testing"

func TestEventsFutureCursorRequiresResync(t *testing.T) {
	m := newTestManager(t)
	for _, after := range []uint64{0, m.revision, m.revision + 1} {
		p := m.Events(after)
		if p.ResyncRequired != (after > m.revision) || p.Cursor != m.revision {
			t.Fatalf("after=%d page=%+v", after, p)
		}
	}
}
