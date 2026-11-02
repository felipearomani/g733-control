// Package hidpp is a small Logitech HID++ 2.0 client.
//
// One background goroutine reads every input report from the device and
// routes it either to the request waiting for it (matched by feature index,
// function and software id) or to the notification channel, so unsolicited
// events (battery updates, power changes) never get mistaken for replies.
package hidpp

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"
)

const (
	reportShort = 0x10
	reportLong  = 0x11
	longLen     = 20
	// PayloadLen is the number of parameter bytes in a long HID++ report.
	PayloadLen  = longLen - 4
	errShortSub = 0x8F // HID++ 1.0 error report
	errLongSub  = 0xFF // HID++ 2.0 error report

	readPoll = 100 * time.Millisecond
	// DefaultTimeout is how long a request waits for its reply.
	DefaultTimeout = 800 * time.Millisecond
)

var (
	ErrNotFound    = errors.New("device not found")
	ErrTimeout     = errors.New("no reply from headset")
	ErrClosed      = errors.New("device closed")
	ErrDeviceLost  = errors.New("device disconnected")
	ErrUnsupported = errors.New("feature not supported")
)

var errorNames = map[byte]string{
	0x01: "Unknown",
	0x02: "InvalidArgument",
	0x03: "OutOfRange",
	0x04: "HWError",
	0x05: "LogitechInternal",
	0x06: "InvalidFeatureIndex",
	0x07: "InvalidFunctionID",
	0x08: "Busy",
	0x09: "Unsupported",
}

// Error is an error report sent back by the device.
type Error struct {
	FeatureIndex byte
	Function     byte
	Code         byte
}

func (e *Error) Error() string {
	name := errorNames[e.Code]
	if name == "" {
		name = "?"
	}
	return fmt.Sprintf("HID++ error 0x%02x (%s) on feature #%d fn %d", e.Code, name, e.FeatureIndex, e.Function)
}

// Notification is an input report the device sent on its own.
type Notification struct {
	FeatureIndex byte
	Function     byte
	Data         []byte // always PayloadLen bytes
}

// Feature describes one entry of the device's feature table.
type Feature struct {
	ID      uint16
	Index   byte
	Version byte
}

// Transport is the raw HID connection. ReadWithTimeout must return (0, nil)
// when nothing arrived before the timeout.
type Transport interface {
	Write(p []byte) (int, error)
	ReadWithTimeout(p []byte, timeout time.Duration) (int, error)
	Close() error
}

// Device is a HID++ 2.0 device reachable through a Transport.
type Device struct {
	t           Transport
	deviceIndex byte
	timeout     time.Duration
	// swID tags our requests (1-15; 0 means "device event"). It is random per
	// process so two g733 processes sharing the dongle never take each
	// other's replies.
	swID byte

	reqMu   sync.Mutex
	replies chan []byte
	notes   chan Notification

	cacheMu  sync.Mutex
	features map[uint16]Feature

	done      chan struct{}
	closeOnce sync.Once
	readerWG  sync.WaitGroup

	failed   chan struct{}
	failOnce sync.Once
	failErr  error
}

// NewDevice starts the reader goroutine on t.
func NewDevice(t Transport, deviceIndex byte, timeout time.Duration) *Device {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	d := &Device{
		t:           t,
		deviceIndex: deviceIndex,
		timeout:     timeout,
		swID:        byte(rand.IntN(15)) + 1,
		replies:     make(chan []byte, 16),
		notes:       make(chan Notification, 64),
		features:    map[uint16]Feature{},
		done:        make(chan struct{}),
		failed:      make(chan struct{}),
	}
	d.readerWG.Add(1)
	go d.readLoop()
	return d
}

// Notifications delivers unsolicited device events. It is never closed;
// select on Done as well.
func (d *Device) Notifications() <-chan Notification { return d.notes }

// Done is closed when the device is closed.
func (d *Device) Done() <-chan struct{} { return d.done }

// Close stops the reader and releases the transport. Safe to call twice.
func (d *Device) Close() error {
	var err error
	d.closeOnce.Do(func() {
		close(d.done)
		d.readerWG.Wait()
		err = d.t.Close()
	})
	return err
}

func (d *Device) fail(err error) {
	d.failOnce.Do(func() {
		d.failErr = fmt.Errorf("%w: %v", ErrDeviceLost, err)
		close(d.failed)
	})
}

func (d *Device) alive() error {
	select {
	case <-d.done:
		return ErrClosed
	case <-d.failed:
		return d.failErr
	default:
		return nil
	}
}

func (d *Device) readLoop() {
	defer d.readerWG.Done()
	buf := make([]byte, 64)
	for {
		select {
		case <-d.done:
			return
		default:
		}
		n, err := d.t.ReadWithTimeout(buf, readPoll)
		if err != nil {
			select {
			case <-d.done: // read failed because we are closing
			default:
				d.fail(err)
			}
			return
		}
		if n >= 4 {
			d.dispatch(append([]byte(nil), buf[:n]...))
		}
	}
}

func (d *Device) dispatch(p []byte) {
	if (p[0] != reportShort && p[0] != reportLong) || p[1] != d.deviceIndex {
		return // not HID++ (media keys etc.) or another device index
	}
	if p[2] == errLongSub || p[2] == errShortSub || p[3]&0x0F == d.swID {
		select {
		case d.replies <- p:
		default: // nobody waiting and buffer full: drop
		}
		return
	}
	n := Notification{FeatureIndex: p[2], Function: p[3] >> 4, Data: pad(p[4:])}
	select {
	case d.notes <- n:
	default: // consumer too slow: drop the oldest-style overflow
	}
}

func pad(b []byte) []byte {
	out := make([]byte, PayloadLen)
	copy(out, b)
	return out
}

// Request sends one HID++ 2.0 call and returns the reply payload, which is
// always PayloadLen bytes long (zero padded) so callers can index it safely.
func (d *Device) Request(featureIndex, function byte, params ...byte) ([]byte, error) {
	if len(params) > PayloadLen {
		return nil, fmt.Errorf("too many parameters (%d > %d)", len(params), PayloadLen)
	}
	d.reqMu.Lock()
	defer d.reqMu.Unlock()
	if err := d.alive(); err != nil {
		return nil, err
	}
	for drained := false; !drained; { // stale replies from timed-out requests
		select {
		case <-d.replies:
		default:
			drained = true
		}
	}

	fnSw := (function&0x0F)<<4 | d.swID
	msg := make([]byte, longLen)
	msg[0], msg[1], msg[2], msg[3] = reportLong, d.deviceIndex, featureIndex, fnSw
	copy(msg[4:], params)
	if _, err := d.t.Write(msg); err != nil {
		d.fail(err)
		return nil, d.failErr
	}

	timer := time.NewTimer(d.timeout)
	defer timer.Stop()
	for {
		select {
		case p := <-d.replies:
			if len(p) < 4 {
				continue
			}
			isErr := p[2] == errLongSub || p[2] == errShortSub
			switch {
			case isErr && len(p) >= 6 && p[3] == featureIndex && p[4] == fnSw:
				return nil, &Error{FeatureIndex: featureIndex, Function: function, Code: p[5]}
			case !isErr && p[2] == featureIndex && p[3] == fnSw:
				return pad(p[4:]), nil
			}
		case <-timer.C:
			return nil, ErrTimeout
		case <-d.done:
			return nil, ErrClosed
		case <-d.failed:
			return nil, d.failErr
		}
	}
}

// Ping checks that the device answers (root getProtocolVersion).
func (d *Device) Ping() error {
	const token = 0x5A
	r, err := d.Request(0x00, 1, 0, 0, token)
	if err != nil {
		return err
	}
	if r[2] != token {
		return fmt.Errorf("ping: unexpected token 0x%02x", r[2])
	}
	return nil
}

// Feature resolves a feature ID to its index (cached).
func (d *Device) Feature(id uint16) (Feature, error) {
	if id == 0x0000 {
		return Feature{ID: 0, Index: 0}, nil
	}
	d.cacheMu.Lock()
	f, ok := d.features[id]
	d.cacheMu.Unlock()
	if ok {
		return f, nil
	}
	r, err := d.Request(0x00, 0, byte(id>>8), byte(id))
	if err != nil {
		return Feature{}, err
	}
	if r[0] == 0 {
		return Feature{}, fmt.Errorf("%w: 0x%04x", ErrUnsupported, id)
	}
	f = Feature{ID: id, Index: r[0], Version: r[2]}
	d.cacheMu.Lock()
	d.features[id] = f
	d.cacheMu.Unlock()
	return f, nil
}

// ListFeatures walks the FeatureSet (0x0001) table.
func (d *Device) ListFeatures() ([]Feature, error) {
	fs, err := d.Feature(0x0001)
	if err != nil {
		return nil, err
	}
	r, err := d.Request(fs.Index, 0)
	if err != nil {
		return nil, err
	}
	out := []Feature{{ID: 0x0000, Index: 0}}
	for i := 1; i <= int(r[0]); i++ {
		e, err := d.Request(fs.Index, 1, byte(i))
		if err != nil {
			return out, err
		}
		id := uint16(e[0])<<8 | uint16(e[1])
		f := Feature{ID: id, Index: byte(i)}
		if full, err := d.Feature(id); err == nil {
			f.Version = full.Version
		}
		out = append(out, f)
	}
	return out, nil
}
