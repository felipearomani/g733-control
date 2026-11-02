package hidpp

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeTransport answers every write through a responder func.
type fakeTransport struct {
	mu      sync.Mutex
	in      chan []byte
	respond func(req []byte) [][]byte
	writes  [][]byte
	closed  bool
}

func newFake(respond func(req []byte) [][]byte) *fakeTransport {
	return &fakeTransport{in: make(chan []byte, 32), respond: respond}
}

func (f *fakeTransport) Write(p []byte) (int, error) {
	f.mu.Lock()
	f.writes = append(f.writes, append([]byte(nil), p...))
	f.mu.Unlock()
	for _, r := range f.respond(p) {
		f.in <- r
	}
	return len(p), nil
}

func (f *fakeTransport) ReadWithTimeout(p []byte, timeout time.Duration) (int, error) {
	select {
	case r := <-f.in:
		return copy(p, r), nil
	case <-time.After(timeout):
		return 0, nil
	}
}

func (f *fakeTransport) Close() error { f.closed = true; return nil }

func long(b ...byte) []byte {
	out := make([]byte, 20)
	copy(out, b)
	return out
}

func TestRequestMatchesReplyAndSkipsNotifications(t *testing.T) {
	ft := newFake(func(req []byte) [][]byte {
		return [][]byte{
			long(0x11, 0xFF, 0x08, 0x00, 0x0e, 0x97, 0x01), // battery event (swid 0)
			long(0x11, 0xFF, req[2], req[3], 0xAA, 0xBB),   // our reply
		}
	})
	d := NewDevice(ft, 0xFF, 200*time.Millisecond)
	defer d.Close()

	r, err := d.Request(0x07, 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != PayloadLen || r[0] != 0xAA || r[1] != 0xBB {
		t.Fatalf("unexpected reply % x", r)
	}
	select {
	case n := <-d.Notifications():
		if n.FeatureIndex != 0x08 || n.Data[0] != 0x0e || n.Data[1] != 0x97 {
			t.Fatalf("bad notification %+v", n)
		}
	case <-time.After(time.Second):
		t.Fatal("notification not delivered")
	}
	w := ft.writes[0]
	if w[0] != 0x11 || w[1] != 0xFF || w[2] != 0x07 || w[3]>>4 != 1 || w[3]&0x0F == 0 || w[4] != 50 || len(w) != 20 {
		t.Fatalf("bad request % x", w)
	}
}

func TestRequestReturnsDeviceError(t *testing.T) {
	ft := newFake(func(req []byte) [][]byte {
		return [][]byte{long(0x11, 0xFF, 0xFF, req[2], req[3], 0x02)}
	})
	d := NewDevice(ft, 0xFF, 200*time.Millisecond)
	defer d.Close()

	_, err := d.Request(0x06, 2, 1)
	var he *Error
	if !errors.As(err, &he) || he.Code != 0x02 {
		t.Fatalf("want InvalidArgument error, got %v", err)
	}
}

func TestRequestTimesOut(t *testing.T) {
	d := NewDevice(newFake(func([]byte) [][]byte { return nil }), 0xFF, 50*time.Millisecond)
	defer d.Close()
	if _, err := d.Request(0x01, 0); !errors.Is(err, ErrTimeout) {
		t.Fatalf("want timeout, got %v", err)
	}
}

func TestFeatureLookupIsCachedAndUnsupportedDetected(t *testing.T) {
	calls := 0
	ft := newFake(func(req []byte) [][]byte {
		calls++
		idx := byte(0x04)
		if req[4] == 0x99 {
			idx = 0 // unknown feature
		}
		return [][]byte{long(0x11, 0xFF, req[2], req[3], idx, 0x00, 0x03)}
	})
	d := NewDevice(ft, 0xFF, 200*time.Millisecond)
	defer d.Close()

	for i := 0; i < 3; i++ {
		f, err := d.Feature(0x8070)
		if err != nil || f.Index != 4 || f.Version != 3 {
			t.Fatalf("got %+v %v", f, err)
		}
	}
	if calls != 1 {
		t.Fatalf("feature lookup not cached: %d calls", calls)
	}
	if _, err := d.Feature(0x9999); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("want ErrUnsupported, got %v", err)
	}
}

func TestClosedDeviceRejectsRequests(t *testing.T) {
	d := NewDevice(newFake(func([]byte) [][]byte { return nil }), 0xFF, 50*time.Millisecond)
	_ = d.Close()
	_ = d.Close() // idempotent
	if _, err := d.Request(0, 0); !errors.Is(err, ErrClosed) {
		t.Fatalf("want ErrClosed, got %v", err)
	}
}
