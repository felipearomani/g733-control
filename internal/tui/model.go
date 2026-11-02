// Package tui is the interactive Bubble Tea interface for the G733.
package tui

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/felipearomani/g733-control/internal/g733"
	"github.com/felipearomani/g733-control/internal/hidpp"
	"github.com/felipearomani/g733-control/internal/i18n"
)

type tabID int

const (
	tabLights tabID = iota
	tabEQ
	tabAudio
	tabInfo
)

var tabTitles = []string{"Lights", "Equalizer", "Audio", "Info"} // translated at render

type connState int

const (
	connSearching connState = iota
	connNoDongle
	connOffline
	connOnline
)

const (
	kindLights   = "lights"
	kindEQ       = "eq"
	kindSidetone = "sidetone"
	kindAutoOff  = "autooff"

	debounce     = 120 * time.Millisecond
	batteryEvery = 30 * time.Second
	retryEvery   = 2 * time.Second
	frameEvery   = 80 * time.Millisecond
)

// snapshot is everything read from the headset in one load.
type snapshot struct {
	name     string
	firmware []g733.Firmware
	features []g733.FeatureInfo

	adcIndex   byte
	hasADC     bool
	battery    g733.Battery
	batteryErr error

	zones          []g733.Zone
	lights         map[byte]g733.Light
	lightsReadable bool
	lightsErr      error

	eqInfo g733.EQInfo
	eq     []int
	eqErr  error

	sidetone    int
	sidetoneErr error
	autoOff     int
	autoOffErr  error
}

type (
	connectedMsg struct {
		h   *g733.Headset
		err error
	}
	loadedMsg struct {
		h    *g733.Headset
		gen  int
		snap snapshot
		err  error
	}
	retryMsg       struct{ gen int }
	batteryTickMsg struct{ gen int }
	batteryMsg     struct {
		h   *g733.Headset
		b   g733.Battery
		err error
	}
	noteMsg struct {
		h *g733.Headset
		n hidpp.Notification
	}
	debounceMsg struct {
		kind string
		seq  uint64
	}
	appliedMsg struct {
		kind    string
		skipped bool
		err     error
		persist bool
		light   g733.Light
		zones   []byte
		value   int
	}
	frameMsg time.Time
)

// applier runs device writes one at a time and drops stale ones, so a slow
// write can never land after a newer value.
type applier struct {
	mu     sync.Mutex
	latest map[string]uint64
}

func (a *applier) do(kind string, seq uint64, f func() error) (skipped bool, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if seq < a.latest[kind] {
		return true, nil
	}
	a.latest[kind] = seq
	return false, f()
}

type statusKind int

const (
	stInfo statusKind = iota
	stOK
	stErr
)

// Model is the Bubble Tea model.
type Model struct {
	h       *g733.Headset
	conn    connState
	connErr error
	gen     int

	width, height int
	tab           tabID
	spin          spinner.Model
	showHelp      bool
	now           time.Time

	status     string
	statusKind statusKind

	name       string
	productID  uint16
	firmware   []g733.Firmware
	features   []g733.FeatureInfo
	adcIndex   byte
	hasADC     bool
	battery    g733.Battery
	hasBattery bool

	lights lightsTab
	eq     eqTab
	audio  audioTab

	seq map[string]uint64
	ap  *applier
}

func newModel() Model {
	return Model{
		spin:   spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(lipgloss.NewStyle().Foreground(colAccent))),
		lights: newLightsTab(),
		seq:    map[string]uint64{},
		ap:     &applier{latest: map[string]uint64{}},
		now:    time.Now(),
	}
}

// Run starts the full-screen interface.
func Run() error {
	final, err := tea.NewProgram(newModel(), tea.WithAltScreen()).Run()
	if m, ok := final.(Model); ok && m.h != nil {
		_ = m.h.Close()
	}
	return err
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spin.Tick, connectCmd(), frameCmd())
}

// --- commands -----------------------------------------------------------------

func connectCmd() tea.Cmd {
	return func() tea.Msg {
		h, err := g733.Open()
		return connectedMsg{h: h, err: err}
	}
}

func retryCmd(gen int) tea.Cmd {
	return tea.Tick(retryEvery, func(time.Time) tea.Msg { return retryMsg{gen} })
}

func frameCmd() tea.Cmd {
	return tea.Tick(frameEvery, func(t time.Time) tea.Msg { return frameMsg(t) })
}

func batteryTick(gen int) tea.Cmd {
	return tea.Tick(batteryEvery, func(time.Time) tea.Msg { return batteryTickMsg{gen} })
}

func batteryCmd(h *g733.Headset) tea.Cmd {
	return func() tea.Msg {
		b, err := h.Battery()
		return batteryMsg{h: h, b: b, err: err}
	}
}

func waitNote(h *g733.Headset) tea.Cmd {
	return func() tea.Msg {
		select {
		case n := <-h.Notifications():
			return noteMsg{h: h, n: n}
		case <-h.Done():
			return nil
		}
	}
}

func loadCmd(h *g733.Headset, gen int) tea.Cmd {
	return func() tea.Msg {
		if err := h.Ping(); err != nil {
			return loadedMsg{h: h, gen: gen, err: err}
		}
		var s snapshot
		s.name, _ = h.Name()
		s.firmware, _ = h.Firmware()
		s.features, _ = h.Features()
		if idx, err := h.FeatureIndex(g733.FeatADC); err == nil {
			s.adcIndex, s.hasADC = idx, true
		}
		s.battery, s.batteryErr = h.Battery()
		s.zones, s.lightsErr = h.Zones()
		s.lights = map[byte]g733.Light{}
		if s.lightsErr == nil {
			s.lightsReadable = h.LightsReadable()
			if s.lightsReadable {
				for _, z := range s.zones {
					if l, err := h.Light(z.Index); err == nil {
						s.lights[z.Index] = l
					}
				}
			}
		}
		s.eqInfo, s.eqErr = h.EQInfo()
		if s.eqErr == nil {
			s.eq, s.eqErr = h.EQ()
		}
		s.sidetone, s.sidetoneErr = h.Sidetone()
		s.autoOff, s.autoOffErr = h.AutoOff()
		return loadedMsg{h: h, gen: gen, snap: s}
	}
}

func (m Model) schedule(kind string) tea.Cmd {
	m.seq[kind]++
	seq := m.seq[kind]
	return tea.Tick(debounce, func(time.Time) tea.Msg { return debounceMsg{kind: kind, seq: seq} })
}

func (m Model) applyNow(kind string, persist bool) tea.Cmd {
	m.seq[kind]++
	return m.applyCmd(kind, m.seq[kind], persist)
}

func (m Model) applyCmd(kind string, seq uint64, persist bool) tea.Cmd {
	h, ap := m.h, m.ap
	if h == nil {
		return nil
	}
	switch kind {
	case kindLights:
		l, zones := m.lights.edit, m.lights.targetZones()
		return func() tea.Msg {
			var done []byte
			skipped, err := ap.do(kind, seq, func() error {
				if len(zones) == 0 {
					return i18n.Errorf("no zone supports %s", l.Effect.Label())
				}
				for _, z := range zones {
					if err := h.SetLight(z, l, persist); err != nil {
						return err
					}
					done = append(done, z)
				}
				return nil
			})
			return appliedMsg{kind: kind, skipped: skipped, err: err, persist: persist, light: l, zones: done}
		}
	case kindEQ:
		gains := append([]int(nil), m.eq.gains...)
		return func() tea.Msg {
			skipped, err := ap.do(kind, seq, func() error { return h.SetEQ(gains) })
			return appliedMsg{kind: kind, skipped: skipped, err: err}
		}
	case kindSidetone:
		v := m.audio.sidetone
		return func() tea.Msg {
			skipped, err := ap.do(kind, seq, func() error { return h.SetSidetone(v) })
			return appliedMsg{kind: kind, skipped: skipped, err: err, value: v}
		}
	case kindAutoOff:
		v := m.audio.autoOff
		return func() tea.Msg {
			skipped, err := ap.do(kind, seq, func() error { return h.SetAutoOff(v) })
			return appliedMsg{kind: kind, skipped: skipped, err: err, value: v}
		}
	}
	return nil
}

// --- update -------------------------------------------------------------------

func (m *Model) setStatus(k statusKind, format string, args ...any) {
	m.status, m.statusKind = i18n.Tf(format, args...), k
}

func isLink(err error) bool {
	return errors.Is(err, hidpp.ErrDeviceLost) || errors.Is(err, hidpp.ErrClosed)
}

func isOffline(err error) bool {
	return errors.Is(err, hidpp.ErrTimeout) || isLink(err)
}

// deviceDown moves to the right offline state and schedules a retry.
func (m Model) deviceDown(err error) (Model, tea.Cmd) {
	m.gen++
	m.connErr = err
	if isLink(err) {
		if m.h != nil {
			_ = m.h.Close()
		}
		m.h = nil
		m.conn = connNoDongle
	} else {
		m.conn = connOffline
	}
	return m, retryCmd(m.gen)
}

func (m *Model) applySnapshot(s snapshot) {
	m.name, m.firmware, m.features = s.name, s.firmware, s.features
	m.adcIndex, m.hasADC = s.adcIndex, s.hasADC
	if s.batteryErr == nil {
		m.battery, m.hasBattery = s.battery, true
	}
	m.lights.load(s)
	m.eq.load(s)
	m.audio.load(s)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case frameMsg:
		m.now = time.Time(msg)
		return m, frameCmd()

	case connectedMsg:
		if msg.err != nil {
			m.conn, m.connErr = connNoDongle, msg.err
			m.gen++
			return m, retryCmd(m.gen)
		}
		m.h, m.productID = msg.h, msg.h.ProductID
		m.conn = connSearching
		m.gen++
		return m, tea.Batch(loadCmd(m.h, m.gen), waitNote(m.h))

	case retryMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		if m.h == nil {
			return m, connectCmd()
		}
		return m, loadCmd(m.h, m.gen)

	case loadedMsg:
		if msg.h != m.h || msg.gen != m.gen {
			return m, nil
		}
		if msg.err != nil {
			return m.deviceDown(msg.err)
		}
		wasOnline := m.conn == connOnline
		m.applySnapshot(msg.snap)
		m.conn, m.connErr = connOnline, nil
		if !wasOnline {
			m.setStatus(stOK, "Connected to %s", m.name)
		}
		return m, batteryTick(m.gen)

	case batteryTickMsg:
		if msg.gen != m.gen || m.conn != connOnline || m.h == nil {
			return m, nil
		}
		return m, tea.Batch(batteryCmd(m.h), batteryTick(m.gen))

	case batteryMsg:
		if msg.h != m.h {
			return m, nil
		}
		if msg.err != nil {
			if isOffline(msg.err) {
				return m.deviceDown(msg.err)
			}
			return m, nil
		}
		m.battery, m.hasBattery = msg.b, true
		if !msg.b.Active {
			return m.deviceDown(hidpp.ErrTimeout)
		}
		return m, nil

	case noteMsg:
		if msg.h != m.h || m.h == nil {
			return m, nil
		}
		cmds := []tea.Cmd{waitNote(m.h)}
		if m.hasADC && msg.n.FeatureIndex == m.adcIndex && msg.n.Function == 0 {
			b := g733.ParseBattery(msg.n.Data)
			if b.MilliVolts > 0 {
				m.battery, m.hasBattery = b, true
			}
		}
		if m.conn == connOffline { // the headset woke up: reload everything
			m.gen++
			cmds = append(cmds, loadCmd(m.h, m.gen))
		}
		return m, tea.Batch(cmds...)

	case debounceMsg:
		if msg.seq != m.seq[msg.kind] || m.conn != connOnline {
			return m, nil
		}
		return m, m.applyCmd(msg.kind, msg.seq, false)

	case appliedMsg:
		return m.handleApplied(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleApplied(a appliedMsg) (tea.Model, tea.Cmd) {
	if a.skipped {
		return m, nil
	}
	if a.err != nil {
		if isOffline(a.err) {
			m.setStatus(stErr, "The headset did not answer: %v", a.err)
			return m.deviceDown(a.err)
		}
		m.setStatus(stErr, "%v", a.err)
		return m, nil
	}
	switch a.kind {
	case kindLights:
		var names []string
		for _, z := range a.zones {
			m.lights.state[z] = a.light
			for _, zz := range m.lights.zones {
				if zz.Index == z {
					names = append(names, zz.Name())
				}
			}
		}
		if a.light.Effect != g733.EffectOff {
			m.lights.lastOn = a.light
		}
		suffix := i18n.T("live")
		if a.persist {
			suffix = i18n.T("saved to headset")
		}
		m.setStatus(stOK, "Lights → %s  [%s] (%s)", a.light.String(), strings.Join(names, ", "), suffix)
	case kindEQ:
		m.setStatus(stOK, "Equalizer applied (%s)", m.eq.presetName())
	case kindSidetone:
		m.setStatus(stOK, "Sidetone at %d%%", a.value)
	case kindAutoOff:
		m.setStatus(stOK, "Auto power-off: %s", autoOffLabel(a.value))
	}
	return m, nil
}

func (m Model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := k.String()
	switch s {
	case "ctrl+c":
		return m, tea.Quit
	case "tab":
		m.tab = (m.tab + 1) % tabID(len(tabTitles))
		m.lights.setFocusIfTyping()
		return m, nil
	case "shift+tab":
		m.tab = (m.tab + tabID(len(tabTitles)) - 1) % tabID(len(tabTitles))
		m.lights.setFocusIfTyping()
		return m, nil
	}
	if m.tab == tabLights && m.lights.typing() && m.conn == connOnline {
		return m.lightsKey(k)
	}
	switch s {
	case "q":
		return m, tea.Quit
	case "1", "2", "3", "4":
		m.tab = tabID(s[0] - '1')
		return m, nil
	case "?":
		m.showHelp = !m.showHelp
		return m, nil
	case "i":
		lang := i18n.Toggle()
		_ = i18n.Save(lang) // best effort: the choice also works for this session only
		m.setStatus(stInfo, "Language: %s", lang.Name())
		return m, nil
	case "r":
		m.gen++
		m.setStatus(stInfo, "Reloading headset state…")
		if m.h == nil {
			return m, connectCmd()
		}
		return m, loadCmd(m.h, m.gen)
	}
	if m.conn != connOnline {
		return m, nil
	}
	switch m.tab {
	case tabLights:
		return m.lightsKey(k)
	case tabEQ:
		if m.eq.key(s) {
			return m, m.schedule(kindEQ)
		}
	case tabAudio:
		if kind := m.audio.key(s); kind != "" {
			return m, m.schedule(kind)
		}
	}
	return m, nil
}

func (t *lightsTab) setFocusIfTyping() {
	if t.typing() {
		t.setFocus(lfEffect)
	}
}

func (m Model) lightsKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	t := &m.lights
	if len(t.zones) == 0 {
		return m, nil
	}
	s := k.String()
	if t.typing() {
		switch s {
		case "up", "down", "esc":
			// leave the text field and navigate below
		case "enter":
			t.setFocus(lfHex) // re-sync the field with the applied color
			return m, m.applyNow(kindLights, true)
		default:
			var cmd tea.Cmd
			t.hex, cmd = t.hex.Update(k)
			v := t.hex.Value()
			if len(v) == 6 {
				if c, err := g733.ParseColor(v); err == nil && c != t.edit.Color {
					t.edit.Color = c
					t.syncFromRGB()
					return m, tea.Batch(cmd, m.schedule(kindLights))
				}
			}
			return m, cmd
		}
	}
	fields := t.fields()
	pos := 0
	for i, f := range fields {
		if f == t.focus {
			pos = i
		}
	}
	switch s {
	case "up", "k":
		t.setFocus(fields[(pos-1+len(fields))%len(fields)])
	case "down", "j":
		t.setFocus(fields[(pos+1)%len(fields)])
	case "esc":
		if t.typing() {
			t.setFocus(lfBright)
		}
	case "left", "h", "right", "l", "shift+left", "shift+right", "H", "L":
		dir := 1
		if s == "left" || s == "h" || s == "shift+left" || s == "H" {
			dir = -1
		}
		big := strings.HasPrefix(s, "shift+") || s == "H" || s == "L"
		if t.adjust(dir, big) {
			return m, m.schedule(kindLights)
		}
	case "enter", "s":
		return m, m.applyNow(kindLights, true)
	case "o":
		t.toggle()
		return m, m.applyNow(kindLights, true)
	}
	return m, nil
}

// --- view ---------------------------------------------------------------------

func (m Model) View() string {
	width := m.width
	if width <= 0 {
		width = 90
	}
	inner := min(width-2, 96)

	var body string
	switch m.conn {
	case connOnline:
		switch m.tab {
		case tabLights:
			body = m.lights.view(inner, m.now)
		case tabEQ:
			body = m.eq.view()
		case tabAudio:
			body = m.audio.view(inner)
		case tabInfo:
			body = infoView(m)
		}
	default:
		body = m.connView()
	}
	if m.showHelp {
		body = helpView()
	}
	parts := []string{m.headerView(inner), m.tabsView(), "", body, "", m.statusView(), m.footerView(inner)}
	return lipgloss.NewStyle().Padding(1, 1, 0, 1).Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}

func (m Model) headerView(width int) string {
	name := m.name
	if name == "" {
		name = "Logitech G733"
	}
	left := sTitle.Render("◉ " + name)

	var conn string
	switch m.conn {
	case connOnline:
		conn = sOK.Render("● " + i18n.T("online"))
	case connOffline:
		conn = sWarn.Render("● " + i18n.T("headset off"))
	case connNoDongle:
		conn = sErr.Render("● " + i18n.T("no dongle"))
	default:
		conn = sMuted.Render(m.spin.View() + i18n.T("connecting"))
	}
	right := conn
	if m.hasBattery && m.conn == connOnline {
		b := m.battery
		pct := lipgloss.NewStyle().Foreground(batteryColor(b.Percent)).Bold(true).Render(fmt.Sprintf("%d%%", b.Percent))
		extra := fmt.Sprintf(" %.2f V", float64(b.MilliVolts)/1000)
		if b.Charging {
			extra += " " + i18n.T("charging")
		}
		right = conn + "   " + sMuted.Render(i18n.T("BAT")+" ") + pct + " " + batteryBar(b.Percent, 10) + sMuted.Render(extra)
	}
	gap := max(width-lipgloss.Width(left)-lipgloss.Width(right), 2)
	return left + strings.Repeat(" ", gap) + right
}

func (m Model) tabsView() string {
	var tabs []string
	for i, t := range tabTitles {
		label := fmt.Sprintf("%d %s", i+1, i18n.T(t))
		if tabID(i) == m.tab {
			tabs = append(tabs, sTabOn.Render(label))
		} else {
			tabs = append(tabs, sTabOff.Render(label))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
}

func (m Model) connView() string {
	var title, detail string
	switch m.conn {
	case connNoDongle:
		title = i18n.T("LIGHTSPEED dongle not found")
		detail = i18n.T("Plug in the G733 USB receiver. Retrying every 2 s…")
	case connOffline:
		title = i18n.T("Headset off or out of range")
		detail = i18n.T("Turn the G733 on — this screen refreshes by itself when it answers.")
	default:
		title = m.spin.View() + " " + i18n.T("Connecting to the headset…")
	}
	lines := []string{sTitle.Render(title)}
	if detail != "" {
		lines = append(lines, sMuted.Render(detail))
	}
	if m.connErr != nil {
		lines = append(lines, "", sMuted.Render(i18n.T("detail: ")+m.connErr.Error()))
	}
	return sPanel.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m Model) statusView() string {
	if m.status == "" {
		return " "
	}
	switch m.statusKind {
	case stOK:
		return sOK.Render("✓ ") + sText.Render(m.status)
	case stErr:
		return sErr.Render("✗ " + m.status)
	}
	return sMuted.Render("· " + m.status)
}

func (m Model) footerView(width int) string {
	k := func(key, desc string) string { return sKey.Render(key) + " " + sMuted.Render(i18n.T(desc)) }
	var items []string
	typing := m.conn == connOnline && m.tab == tabLights && m.lights.typing()
	switch {
	case m.conn != connOnline:
	case typing:
		items = []string{k("0-9 a-f", "color"), k("enter", "save"), k("↑↓/esc", "leave field"), k("tab", "tab"), k("ctrl+c", "quit")}
	case m.tab == tabLights:
		items = []string{k("↑↓", "field"), k("←→", "adjust"), k("⇧←→", "big step"), k("enter", "save"), k("o", "on/off")}
	case m.tab == tabEQ:
		items = []string{k("←→", "band"), k("↑↓", "±1 dB"), k("⇧↑↓", "±3 dB"), k("p/P", "preset"), k("0", "reset band"), k("f", "flat")}
	case m.tab == tabAudio:
		items = []string{k("↑↓", "field"), k("←→", "adjust"), k("⇧←→", "big step")}
	}
	if !typing {
		items = append(items, k("tab", "tab"), k("r", "reload"), k("i", "language"), k("?", "help"), k("q", "quit"))
	}

	sep := sMuted.Render("  •  ")
	var lines []string
	line := ""
	for _, it := range items {
		switch {
		case line == "":
			line = it
		case lipgloss.Width(line+sep+it) > width:
			lines = append(lines, line)
			line = it
		default:
			line += sep + it
		}
	}
	return strings.Join(append(lines, line), "\n")
}

func helpView() string {
	k := func(key, desc string) string { return "  " + sKey.Width(14).Render(key) + sText.Render(i18n.T(desc)) }
	return sPanel.Render(lipgloss.JoinVertical(lipgloss.Left,
		sTitle.Render(i18n.T("Shortcuts")),
		"",
		k("tab / 1-4", "switch tab"),
		k("↑ ↓  (k j)", "move between fields"),
		k("← →  (h l)", "adjust value / change option"),
		k("⇧← ⇧→ (H L)", "big step"),
		k("r", "re-read everything from the headset"),
		k("i", "switch language (English / Português)"),
		k("q / ctrl+c", "quit"),
		"",
		sTitle.Render(i18n.T("Lights")),
		k("enter / s", "save to the headset (survives power-off)"),
		k("o", "turn the lights on/off"),
		k("Hex", "type RRGGBB; applies at 6 digits"),
		sMuted.Render("  "+i18n.T("Changes apply live; enter makes them permanent.")),
		"",
		sTitle.Render(i18n.T("Equalizer")),
		k("← →", "pick band"),
		k("↑ ↓ / ⇧↑ ⇧↓", "±1 dB / ±3 dB"),
		k("p / P", "next / previous preset"),
		k("0  ·  f", "reset band  ·  all flat"),
		"",
		sMuted.Render(i18n.T("? closes this help")),
	))
}
