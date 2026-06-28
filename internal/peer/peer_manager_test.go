package peer

import (
	"testing"
	"time"
	"torrent-demo/internal/tracker_client"
)

// bf8 builds an 8-bit Bitfield with the given indices set.
func bf8(indices ...int) Bitfield {
	b := NewBitfield(make([]byte, 1), 8)
	for _, i := range indices {
		b.Set(i)
	}
	return *b
}

func TestAddPeer(t *testing.T) {
	pm := NewPeerManager([20]byte{}, [20]byte{}, nil, nil)
	pm.AddPeer(tracker_client.Peer{Ip: "1.2.3.4", Port: 6881})

	mp, ok := pm.managedPeers["tcp://1.2.3.4:6881"]
	if !ok {
		t.Fatalf("AddPeer did not register peer under expected tag; have %v", keysOf(pm))
	}
	if mp.status != StatusIdle {
		t.Errorf("status = %q, want %q", mp.status, StatusIdle)
	}
	if mp.conn != nil {
		t.Errorf("conn = %v, want nil", mp.conn)
	}
	if mp.nbFailure != 0 {
		t.Errorf("nbFailure = %d, want 0", mp.nbFailure)
	}
}

func keysOf(pm *PeerManager) []string {
	out := make([]string, 0, len(pm.managedPeers))
	for k := range pm.managedPeers {
		out = append(out, k)
	}
	return out
}

func TestCanStart(t *testing.T) {
	past := time.Now().Add(-time.Minute)
	future := time.Now().Add(time.Minute)

	tests := []struct {
		name string
		mp   ManagedPeer
		want bool
	}{
		{"idle", ManagedPeer{status: StatusIdle}, true},
		{"connecting", ManagedPeer{status: StatusConnecting}, false},
		{"active", ManagedPeer{status: StatusActive}, false},
		{"failed retry elapsed", ManagedPeer{status: StatusFailed, nbFailure: 1, nextRetry: past}, true},
		{"failed retry pending", ManagedPeer{status: StatusFailed, nbFailure: 1, nextRetry: future}, false},
		{"failed exhausted", ManagedPeer{status: StatusFailed, nbFailure: kMaxNbFailure + 1, nextRetry: past}, false},
		{"later retry elapsed", ManagedPeer{status: StatusLater, nextRetry: past}, true},
		{"later retry pending", ManagedPeer{status: StatusLater, nextRetry: future}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mp := tt.mp
			if got := canStart(&mp); got != tt.want {
				t.Errorf("canStart = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldDelete(t *testing.T) {
	tests := []struct {
		name string
		mp   ManagedPeer
		want bool
	}{
		{"failed over limit", ManagedPeer{status: StatusFailed, nbFailure: kMaxNbFailure + 1}, true},
		{"failed at limit", ManagedPeer{status: StatusFailed, nbFailure: kMaxNbFailure}, false},
		{"active over limit", ManagedPeer{status: StatusActive, nbFailure: kMaxNbFailure + 5}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mp := tt.mp
			if got := shouldDelete(&mp); got != tt.want {
				t.Errorf("shouldDelete = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsStuckInConnecting(t *testing.T) {
	past := time.Now().Add(-time.Minute)
	future := time.Now().Add(time.Minute)

	tests := []struct {
		name string
		mp   ManagedPeer
		want bool
	}{
		{"connecting past grace", ManagedPeer{status: StatusConnecting, afterGracePeriod: past}, true},
		{"connecting within grace", ManagedPeer{status: StatusConnecting, afterGracePeriod: future}, false},
		{"active past grace", ManagedPeer{status: StatusActive, afterGracePeriod: past}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mp := tt.mp
			if got := isStuckInConnecting(&mp); got != tt.want {
				t.Errorf("isStuckInConnecting = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsReady(t *testing.T) {
	tests := []struct {
		name string
		mp   ManagedPeer
		want bool
	}{
		{"connecting and connected", ManagedPeer{status: StatusConnecting, conn: &PeerConnection{connected: true}}, true},
		{"connecting not connected", ManagedPeer{status: StatusConnecting, conn: &PeerConnection{connected: false}}, false},
		{"active and connected", ManagedPeer{status: StatusActive, conn: &PeerConnection{connected: true}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mp := tt.mp
			if got := isReady(&mp); got != tt.want {
				t.Errorf("isReady = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsActive(t *testing.T) {
	tests := []struct {
		name string
		mp   ManagedPeer
		want bool
	}{
		{"active with conn", ManagedPeer{status: StatusActive, conn: &PeerConnection{}}, true},
		{"active no conn", ManagedPeer{status: StatusActive, conn: nil}, false},
		{"connecting with conn", ManagedPeer{status: StatusConnecting, conn: &PeerConnection{}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mp := tt.mp
			if got := isActive(&mp); got != tt.want {
				t.Errorf("isActive = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsSlow(t *testing.T) {
	past := time.Now().Add(-time.Minute)
	future := time.Now().Add(time.Minute)

	// A fresh PeerConnection reports 0 speed on the first sample, which is the
	// condition isSlow keys on once the grace period has elapsed.
	tests := []struct {
		name string
		mp   ManagedPeer
		want bool
	}{
		{"active stalled past grace", ManagedPeer{status: StatusActive, afterGracePeriod: past, conn: &PeerConnection{}}, true},
		{"active within grace", ManagedPeer{status: StatusActive, afterGracePeriod: future, conn: &PeerConnection{}}, false},
		{"connecting past grace", ManagedPeer{status: StatusConnecting, afterGracePeriod: past, conn: &PeerConnection{}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mp := tt.mp
			if got := isSlow(&mp); got != tt.want {
				t.Errorf("isSlow = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResetManagedPeer(t *testing.T) {
	before := time.Now()
	mp := ManagedPeer{
		conn:      &PeerConnection{connected: true},
		status:    StatusActive,
		nbFailure: 5,
		bitfield:  bf8(0, 1, 2),
	}

	resetManagedPeer(&mp)

	if mp.conn != nil {
		t.Errorf("conn = %v, want nil", mp.conn)
	}
	if mp.status != StatusLater {
		t.Errorf("status = %q, want %q", mp.status, StatusLater)
	}
	if mp.nbFailure != 0 {
		t.Errorf("nbFailure = %d, want 0", mp.nbFailure)
	}
	if !mp.nextRetry.After(before) {
		t.Errorf("nextRetry = %v, want after %v", mp.nextRetry, before)
	}
	if !mp.afterGracePeriod.After(before) {
		t.Errorf("afterGracePeriod = %v, want after %v", mp.afterGracePeriod, before)
	}
	if mp.bitfield.Size() != 0 {
		t.Errorf("bitfield size = %d, want 0", mp.bitfield.Size())
	}
}

func TestHandleEventUnknownTag(t *testing.T) {
	pm := NewPeerManager([20]byte{}, [20]byte{}, nil, nil)
	// Should be a no-op (and must not panic) for a tag we never added.
	pm.handleEvent(PeerEvent{tag: "tcp://9.9.9.9:1", kind: BitfieldEvent, payload: bf8(0)})
}

func TestHandleEventBitfield(t *testing.T) {
	pm := NewPeerManager([20]byte{}, [20]byte{}, nil, nil)
	pm.managedPeers["t"] = &ManagedPeer{status: StatusConnecting}

	pm.handleEvent(PeerEvent{tag: "t", kind: BitfieldEvent, payload: bf8(1, 4)})

	got := pm.managedPeers["t"].bitfield
	if !got.Has(1) || !got.Has(4) {
		t.Errorf("bitfield missing expected bits: %v", got.Bytes())
	}
	if got.Has(0) || got.Has(2) {
		t.Errorf("bitfield has unexpected bits set: %v", got.Bytes())
	}
}

func TestHandleEventHave(t *testing.T) {
	pm := NewPeerManager([20]byte{}, [20]byte{}, nil, nil)
	pm.managedPeers["t"] = &ManagedPeer{status: StatusActive, bitfield: bf8()}

	pm.handleEvent(PeerEvent{tag: "t", kind: HaveEvent, payload: 3})

	if !pm.managedPeers["t"].bitfield.Has(3) {
		t.Error("expected bit 3 set after HaveEvent(3)")
	}
}

func TestHandleEventConnectionClosed(t *testing.T) {
	pm := NewPeerManager([20]byte{}, [20]byte{}, nil, nil)
	pm.managedPeers["t"] = &ManagedPeer{
		status:    StatusActive,
		conn:      &PeerConnection{connected: true},
		nbFailure: 2,
		bitfield:  bf8(0, 1),
	}

	pm.handleEvent(PeerEvent{tag: "t", kind: ConnectionClosed})

	mp := pm.managedPeers["t"]
	if mp.status != StatusLater {
		t.Errorf("status = %q, want %q", mp.status, StatusLater)
	}
	if mp.conn != nil {
		t.Errorf("conn = %v, want nil", mp.conn)
	}
}

func TestGetPool(t *testing.T) {
	pm := NewPeerManager([20]byte{}, [20]byte{}, nil, nil)
	// A: Active (eligible), has pieces 0 and 2.
	pm.managedPeers["A"] = &ManagedPeer{
		status:   StatusActive,
		conn:     &PeerConnection{},
		bitfield: bf8(0, 2),
	}
	// B: Connecting + connected => ready (eligible), has pieces 2 and 3.
	pm.managedPeers["B"] = &ManagedPeer{
		status:   StatusConnecting,
		conn:     &PeerConnection{connected: true},
		bitfield: bf8(2, 3),
	}
	// C: Idle (not ready, not active) => excluded entirely.
	pm.managedPeers["C"] = &ManagedPeer{
		status:   StatusIdle,
		bitfield: bf8(0, 1, 2, 3),
	}

	pool := pm.getPool()

	if len(pool) != 8 {
		t.Fatalf("pool length = %d, want 8", len(pool))
	}
	a, b := pm.managedPeers["A"], pm.managedPeers["B"]
	assertPool(t, pool, 0, a)
	assertPool(t, pool, 2, a, b)
	assertPool(t, pool, 3, b)
	assertPool(t, pool, 1) // nobody advertises piece 1
}

func assertPool(t *testing.T, pool [][]*ManagedPeer, idx int, want ...*ManagedPeer) {
	t.Helper()
	got := pool[idx]
	if len(got) != len(want) {
		t.Errorf("pool[%d] has %d peers, want %d", idx, len(got), len(want))
		return
	}
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("pool[%d] missing expected peer %p", idx, w)
		}
	}
}
