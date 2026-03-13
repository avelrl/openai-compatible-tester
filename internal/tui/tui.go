package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/avelrl/openai-compatible-tester/internal/config"
	"github.com/avelrl/openai-compatible-tester/internal/report"
	"github.com/avelrl/openai-compatible-tester/internal/tests"
)

type UI struct {
	screen        tcell.Screen
	runner        *tests.Runner
	cfg           config.Config
	profiles      []config.ModelProfile
	tests         []tests.TestCase
	entries       []entry
	outDir        string
	totalJobs     int
	statusMu      sync.Mutex
	statuses      map[string]tests.Status
	results       []tests.Result
	lastResult    *tests.Result
	doneJobs      int
	paused        bool
	showAnalysis  bool
	exitRequested bool
	selectedIndex int
	detailScroll  int
	detailHScroll int
	listOffset    int
	toast         string
	toastUntil    time.Time

	runCancel    context.CancelFunc
	runDoneCh    chan struct{}
	testEventCh  chan tests.Event
	runErr       error
	finalResults []tests.Result
	runComplete  bool
}

type segment struct {
	text  string
	style tcell.Style
}

type listRow struct {
	Text       string
	EntryIndex int
	IsFirst    bool
	IsGroup    bool
}

var (
	styleBorder      = tcell.StyleDefault.Foreground(tcell.ColorGray)
	styleTitle       = tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorBlue).Bold(true)
	styleHeader      = tcell.StyleDefault.Foreground(tcell.ColorAqua).Bold(true)
	styleLabel       = tcell.StyleDefault.Foreground(tcell.ColorWhite)
	styleDim         = tcell.StyleDefault.Foreground(tcell.ColorGray)
	stylePass        = tcell.StyleDefault.Foreground(tcell.ColorGreen).Bold(true)
	styleFail        = tcell.StyleDefault.Foreground(tcell.ColorRed).Bold(true)
	styleTimeout     = tcell.StyleDefault.Foreground(tcell.ColorOrange).Bold(true)
	styleUnsupported = tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true)
	styleRunning     = tcell.StyleDefault.Foreground(tcell.ColorAqua)
	styleQueued      = tcell.StyleDefault.Foreground(tcell.ColorGray)
	styleKey         = tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true)
	styleSelected    = tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorBlue).Bold(true)
)

func Run(ctx context.Context, runner *tests.Runner, cfg config.Config, profiles []config.ModelProfile, testsList []tests.TestCase, outDir string) ([]tests.Result, error) {
	screen, err := tcell.NewScreen()
	if err != nil {
		return nil, err
	}
	if err := screen.Init(); err != nil {
		return nil, err
	}
	defer screen.Fini()

	ui := &UI{
		screen:    screen,
		runner:    runner,
		cfg:       cfg,
		profiles:  profiles,
		tests:     testsList,
		outDir:    outDir,
		statuses:  map[string]tests.Status{},
		totalJobs: (cfg.Suite.Passes + cfg.Suite.WarmupPasses) * len(profiles) * len(testsList),
	}
	ui.entries = ui.listEntries()
	ui.ensureSelectionValid()

	tcellCh := make(chan tcell.Event, 20)
	go func() {
		for {
			ev := screen.PollEvent()
			tcellCh <- ev
		}
	}()

	ui.startRun(ctx)
	for {
		ui.draw()
		if ui.exitRequested {
			ui.stopRun()
			return ui.results, nil
		}
		select {
		case <-ctx.Done():
			ui.stopRun()
			return ui.results, ctx.Err()
		case ev := <-ui.testEventCh:
			ui.handleTestEvent(ev)
		case <-ui.runDoneCh:
			ui.statusMu.Lock()
			err := ui.runErr
			ui.statusMu.Unlock()
			ui.runDoneCh = nil
			ui.runCancel = nil
			ui.finalizeRunState()
			ui.runComplete = true
			ui.paused = false
			if err != nil {
				ui.setToast("run finished with error")
			} else {
				ui.setToast("run complete: press q to exit, r to rerun")
			}
		case tev := <-tcellCh:
			ui.handleKey(tev)
		}
	}
}

func (ui *UI) startRun(ctx context.Context) {
	ui.resetState()
	ui.testEventCh = make(chan tests.Event, 200)
	ui.runDoneCh = make(chan struct{})
	runCtx, cancel := context.WithCancel(ctx)
	ui.runCancel = cancel
	ui.runComplete = false
	go func() {
		res, err := ui.runner.Run(runCtx, ui.profiles, func(ev tests.Event) {
			select {
			case ui.testEventCh <- ev:
			default:
			}
		})
		ui.statusMu.Lock()
		ui.finalResults = res
		ui.runErr = err
		ui.statusMu.Unlock()
		close(ui.runDoneCh)
	}()
}

func (ui *UI) resetState() {
	ui.statusMu.Lock()
	defer ui.statusMu.Unlock()
	ui.statuses = map[string]tests.Status{}
	ui.results = nil
	ui.finalResults = nil
	ui.lastResult = nil
	ui.doneJobs = 0
	ui.paused = false
	ui.showAnalysis = false
	ui.detailScroll = 0
	ui.detailHScroll = 0
	ui.runComplete = false
	ui.ensureSelectionValid()
}

func (ui *UI) handleTestEvent(ev tests.Event) {
	ui.statusMu.Lock()
	defer ui.statusMu.Unlock()
	key := statusKey(ev.Result.TestID, ev.Result.Profile, ev.Result.Pass)
	switch ev.Type {
	case tests.EventStart:
		ui.statuses[key] = tests.StatusRunning
	case tests.EventFinish:
		ui.statuses[key] = ev.Result.Status
		ui.doneJobs++
		ui.lastResult = &ev.Result
		if ui.runComplete {
			return
		}
		ui.results = append(ui.results, ev.Result)
	}
}

func (ui *UI) finalizeRunState() {
	ui.statusMu.Lock()
	defer ui.statusMu.Unlock()
	ui.results = append([]tests.Result(nil), ui.finalResults...)
	ui.statuses = map[string]tests.Status{}
	ui.doneJobs = 0
	for _, res := range ui.results {
		key := statusKey(res.TestID, res.Profile, res.Pass)
		ui.statuses[key] = res.Status
		ui.doneJobs++
	}
	if len(ui.results) > 0 {
		last := ui.results[len(ui.results)-1]
		ui.lastResult = &last
	} else {
		ui.lastResult = nil
	}
}

func (ui *UI) handleKey(event tcell.Event) {
	switch ev := event.(type) {
	case *tcell.EventKey:
		if ev.Key() == tcell.KeyCtrlC {
			ui.exitRequested = true
			return
		}
		switch ev.Rune() {
		case 'q':
			ui.exitRequested = true
		case 'p':
			if ui.runner.IsPaused() {
				ui.runner.Resume()
				ui.paused = false
			} else {
				ui.runner.Pause()
				ui.paused = true
			}
		case 'a':
			ui.showAnalysis = !ui.showAnalysis
			ui.detailScroll = 0
			ui.detailHScroll = 0
		case 's':
			_ = ui.saveSnapshot()
		case 'r':
			ui.stopRun()
			ui.startRun(context.Background())
		case 'j':
			ui.moveSelection(1)
		case 'k':
			ui.moveSelection(-1)
		case 'd':
			ui.scrollDetails(1)
		case 'u':
			ui.scrollDetails(-1)
		case 'c':
			ui.copyResponseToFile()
		}
		switch ev.Key() {
		case tcell.KeyUp:
			ui.moveSelection(-1)
		case tcell.KeyDown:
			ui.moveSelection(1)
		case tcell.KeyPgDn:
			ui.scrollDetails(1)
		case tcell.KeyPgUp:
			ui.scrollDetails(-1)
		case tcell.KeyLeft:
			if ev.Modifiers()&tcell.ModAlt != 0 {
				ui.scrollDetailsHoriz(-1)
			}
		case tcell.KeyRight:
			if ev.Modifiers()&tcell.ModAlt != 0 {
				ui.scrollDetailsHoriz(1)
			}
		case tcell.KeyHome:
			ui.detailScroll = 0
			ui.detailHScroll = 0
		case tcell.KeyEnd:
			ui.detailScroll = max(0, ui.maxDetailScroll())
		}
	case *tcell.EventResize:
		ui.screen.Sync()
	}
}

func (ui *UI) stopRun() {
	if ui.runner != nil && ui.runner.IsPaused() {
		ui.runner.Resume()
		ui.paused = false
	}
	if ui.runCancel != nil {
		ui.runCancel()
	}
}

func (ui *UI) draw() {
	ui.screen.Clear()
	w, h := ui.screen.Size()
	panelH := h - 1
	if panelH < 3 || w < 10 {
		ui.print(0, 0, styleHeader, "Resize terminal")
		ui.drawBottom(0, h-1, w)
		ui.screen.Show()
		return
	}
	leftW := w / 2
	if leftW < 40 {
		leftW = w
	}
	if leftW < w {
		ui.drawBox(0, 0, leftW, panelH, "Tests")
		ui.drawBox(leftW, 0, w-leftW, panelH, "Details")
		ui.drawLeft(1, 1, leftW-2, panelH-2)
		ui.drawRight(leftW+1, 1, w-leftW-2, panelH-2)
	} else {
		ui.drawBox(0, 0, w, panelH, "Tests")
		ui.drawLeft(1, 1, w-2, panelH-2)
	}
	ui.drawBottom(0, h-1, w)
	ui.screen.Show()
}

func (ui *UI) drawLeft(x, y, w, h int) {
	if w < 10 || h < 3 {
		return
	}
	done, total, progress := ui.progressStats()
	progressText := fmt.Sprintf("Progress: %3.0f%% (%d/%d)", progress*100, done, total)
	ui.print(x, y, styleDim, clipText(progressText, w))
	ui.drawProgressBar(x, y+1, w, progress)

	line := y + 3
	passes := ui.cfg.Suite.Passes + ui.cfg.Suite.WarmupPasses
	passLabels := make([]string, 0, passes)
	for i := 1; i <= passes; i++ {
		label := fmt.Sprintf("P%d", i-ui.cfg.Suite.WarmupPasses)
		if i <= ui.cfg.Suite.WarmupPasses {
			label = fmt.Sprintf("W%d", i)
		}
		passLabels = append(passLabels, label)
	}
	labelWidth := 40
	if w < labelWidth+5 {
		labelWidth = w - 5
		if labelWidth < 10 {
			labelWidth = w
		}
	}
	header := fmt.Sprintf("%-*s %s", labelWidth, "Test", strings.Join(passLabels, " "))
	ui.print(x, line, styleHeader, clipText(header, w))
	line++
	rows := buildListRows(ui.entries, labelWidth)
	visibleRows := h - 3
	if visibleRows < 1 {
		return
	}
	ui.adjustListOffsetRows(rows, visibleRows)
	start := ui.listOffset
	end := min(len(rows), start+visibleRows)
	for rowIdx := start; rowIdx < end; rowIdx++ {
		if line >= y+h {
			break
		}
		row := rows[rowIdx]
		selected := !row.IsGroup && row.EntryIndex == ui.selectedIndex
		if selected {
			ui.fillRow(x, line, w, styleSelected)
		}
		label := padRight(row.Text, labelWidth)
		labelStyle := styleLabel
		if row.IsGroup {
			labelStyle = styleHeader
		} else if selected {
			labelStyle = styleSelected
		}
		ui.print(x, line, labelStyle, label)
		if !row.IsGroup && row.IsFirst {
			entry := ui.entries[row.EntryIndex]
			ui.drawStatusGlyphs(x+labelWidth+1, line, w-(labelWidth+1), entry.TestID, entry.Profile, passes, selected)
		}
		line++
	}
}

func (ui *UI) drawRight(x, y, w, h int) {
	if w < 10 || h < 3 {
		return
	}
	line := y
	if ui.showAnalysis {
		ui.statusMu.Lock()
		snapshot := append([]tests.Result(nil), ui.results...)
		ui.statusMu.Unlock()
		analysis := report.Analyze(snapshot, ui.cfg)
		ui.print(x, line, styleHeader, "Analysis")
		line++
		for _, s := range analysis.Stats {
			if line >= y+h-1 {
				break
			}
			ui.print(x, line, styleLabel, clipText(fmt.Sprintf("%s/%s pass %.2f avg %.1fms", s.TestID, s.Profile, s.PassRate, s.AvgLatency), w))
			line++
		}
		return
	}
	ui.statusMu.Lock()
	snapshot := append([]tests.Result(nil), ui.results...)
	ui.statusMu.Unlock()
	entry, ok := ui.selectedEntry()
	if !ok {
		ui.print(x, line, styleDim, "No tests selected")
		return
	}
	res := latestResult(snapshot, entry.TestID, entry.Profile)
	if res == nil {
		ui.print(x, line, styleDim, "No results yet")
		return
	}
	ui.printSegments(x, line, w, []segment{{text: "Test: ", style: styleDim}, {text: res.TestID, style: styleLabel}})
	line++
	ui.printSegments(x, line, w, []segment{{text: "Profile: ", style: styleDim}, {text: res.Profile, style: styleLabel}})
	line++
	ui.printSegments(x, line, w, []segment{{text: "Status: ", style: styleDim}, {text: strings.ToUpper(string(res.Status)), style: statusStyle(res.Status)}})
	line++
	ui.printSegments(x, line, w, []segment{{text: "HTTP: ", style: styleDim}, {text: fmt.Sprintf("%d", res.HTTPStatus), style: styleLabel}})
	line++
	ui.printSegments(x, line, w, []segment{{text: "Latency: ", style: styleDim}, {text: fmt.Sprintf("%d ms", res.LatencyMS), style: styleLabel}})
	line++

	useWrap := ui.detailHScroll == 0
	var scrollLines []string
	if useWrap {
		scrollLines = buildDetailLinesWrapped(res, w-2)
	} else {
		scrollLines = buildDetailLinesRaw(res)
	}
	available := h - (line - y) - 1
	if available < 1 {
		return
	}
	maxScroll := max(0, len(scrollLines)-available)
	if ui.detailScroll > maxScroll {
		ui.detailScroll = maxScroll
	}
	hMax := ui.maxDetailHScroll()
	if ui.detailHScroll > hMax {
		ui.detailHScroll = hMax
	}
	scrollText := fmt.Sprintf("Scroll: v %d/%d, h %d/%d | HTTP %d", ui.detailScroll+1, maxScroll+1, ui.detailHScroll+1, hMax+1, res.HTTPStatus)
	ui.printSegments(x, line, w, []segment{{text: scrollText, style: styleDim}})
	line++
	start := ui.detailScroll
	end := min(len(scrollLines), start+available)
	for i := start; i < end; i++ {
		lineText := scrollLines[i]
		if !useWrap {
			lineText = sliceLine(scrollLines[i], ui.detailHScroll, w)
		}
		ui.print(x, line, styleLabel, cutText(lineText, w))
		line++
	}
}

func (ui *UI) drawBottom(x, y, w int) {
	segments := []segment{
		{text: "q", style: styleKey}, {text: " quit  ", style: styleDim},
		{text: "r", style: styleKey}, {text: " rerun  ", style: styleDim},
		{text: "p", style: styleKey}, {text: " pause  ", style: styleDim},
		{text: "j/k", style: styleKey}, {text: " select  ", style: styleDim},
		{text: "PgUp/PgDn", style: styleKey}, {text: " vscroll  ", style: styleDim},
		{text: "Alt+←/→", style: styleKey}, {text: " hscroll  ", style: styleDim},
		{text: "c", style: styleKey}, {text: " copy req+resp  ", style: styleDim},
		{text: "a", style: styleKey}, {text: " analysis  ", style: styleDim},
		{text: "s", style: styleKey}, {text: " snapshot", style: styleDim},
	}
	if ui.paused {
		segments = append(segments, segment{text: "  PAUSED", style: styleFail})
	}
	if ui.runComplete {
		segments = append(segments, segment{text: "  RUN COMPLETE", style: stylePass})
	}
	now := time.Now()
	if ui.toast != "" && now.Before(ui.toastUntil) {
		segments = append(segments, segment{text: "  " + ui.toast, style: styleDim})
	}
	ui.printSegments(x, y, w, segments)
}

func (ui *UI) progress() float64 {
	ui.statusMu.Lock()
	defer ui.statusMu.Unlock()
	if ui.totalJobs == 0 {
		return 0
	}
	return float64(ui.doneJobs) / float64(ui.totalJobs)
}

func (ui *UI) progressStats() (int, int, float64) {
	ui.statusMu.Lock()
	defer ui.statusMu.Unlock()
	if ui.totalJobs == 0 {
		return ui.doneJobs, ui.totalJobs, 0
	}
	return ui.doneJobs, ui.totalJobs, float64(ui.doneJobs) / float64(ui.totalJobs)
}

func (ui *UI) moveSelection(delta int) {
	if len(ui.entries) == 0 {
		return
	}
	idx := ui.selectedIndex
	if idx < 0 || idx >= len(ui.entries) {
		idx = 0
	}
	dir := 1
	if delta < 0 {
		dir = -1
	}
	for steps := 0; steps < len(ui.entries); steps++ {
		idx += dir
		if idx < 0 || idx >= len(ui.entries) {
			break
		}
		if !ui.entries[idx].IsGroup {
			ui.selectedIndex = idx
			ui.detailScroll = 0
			ui.detailHScroll = 0
			return
		}
	}
	ui.ensureSelectionValid()
	ui.detailScroll = 0
	ui.detailHScroll = 0
}

func (ui *UI) scrollDetails(dir int) {
	step := 5
	if dir < 0 {
		ui.detailScroll -= step
	} else {
		ui.detailScroll += step
	}
	if ui.detailScroll < 0 {
		ui.detailScroll = 0
	}
	maxScroll := ui.maxDetailScroll()
	if ui.detailScroll > maxScroll {
		ui.detailScroll = maxScroll
	}
}

func (ui *UI) scrollDetailsHoriz(dir int) {
	step := 10
	if dir < 0 {
		ui.detailHScroll -= step
	} else {
		ui.detailHScroll += step
	}
	if ui.detailHScroll < 0 {
		ui.detailHScroll = 0
	}
	maxScroll := ui.maxDetailHScroll()
	if ui.detailHScroll > maxScroll {
		ui.detailHScroll = maxScroll
	}
}

func (ui *UI) selectedEntry() (entry, bool) {
	ui.ensureSelectionValid()
	if len(ui.entries) == 0 {
		return entry{}, false
	}
	if ui.selectedIndex < 0 || ui.selectedIndex >= len(ui.entries) {
		return entry{}, false
	}
	if ui.entries[ui.selectedIndex].IsGroup {
		return entry{}, false
	}
	return ui.entries[ui.selectedIndex], true
}

func (ui *UI) adjustListOffsetRows(rows []listRow, visibleRows int) {
	if visibleRows <= 0 || len(rows) == 0 {
		return
	}
	selectedRow := rowIndexForEntry(rows, ui.selectedIndex)
	if selectedRow < 0 {
		return
	}
	if selectedRow < ui.listOffset {
		ui.listOffset = selectedRow
	}
	if selectedRow >= ui.listOffset+visibleRows {
		ui.listOffset = selectedRow - visibleRows + 1
	}
	if ui.listOffset < 0 {
		ui.listOffset = 0
	}
	if ui.listOffset >= len(rows) {
		ui.listOffset = max(0, len(rows)-1)
	}
}

func (ui *UI) ensureSelectionValid() {
	if len(ui.entries) == 0 {
		ui.selectedIndex = 0
		return
	}
	if ui.selectedIndex < 0 || ui.selectedIndex >= len(ui.entries) {
		ui.selectedIndex = 0
	}
	if !ui.entries[ui.selectedIndex].IsGroup {
		return
	}
	for i := 0; i < len(ui.entries); i++ {
		if !ui.entries[i].IsGroup {
			ui.selectedIndex = i
			return
		}
	}
}

func (ui *UI) maxDetailScroll() int {
	w, h := ui.detailViewport()
	if w <= 0 || h <= 0 {
		return 0
	}
	entry, ok := ui.selectedEntry()
	if !ok {
		return 0
	}
	ui.statusMu.Lock()
	snapshot := append([]tests.Result(nil), ui.results...)
	ui.statusMu.Unlock()
	res := latestResult(snapshot, entry.TestID, entry.Profile)
	if res == nil {
		return 0
	}
	var lines []string
	if ui.detailHScroll == 0 {
		lines = buildDetailLinesWrapped(res, w-2)
	} else {
		lines = buildDetailLinesRaw(res)
	}
	available := h - 5
	if available < 1 {
		return 0
	}
	return max(0, len(lines)-available)
}

func (ui *UI) maxDetailHScroll() int {
	w, _ := ui.detailViewport()
	if w <= 0 {
		return 0
	}
	entry, ok := ui.selectedEntry()
	if !ok {
		return 0
	}
	ui.statusMu.Lock()
	snapshot := append([]tests.Result(nil), ui.results...)
	ui.statusMu.Unlock()
	res := latestResult(snapshot, entry.TestID, entry.Profile)
	if res == nil {
		return 0
	}
	lines := buildDetailLinesRaw(res)
	maxLen := 0
	for _, line := range lines {
		if len(line) > maxLen {
			maxLen = len(line)
		}
	}
	if maxLen <= w {
		return 0
	}
	return maxLen - w
}

func (ui *UI) detailViewport() (int, int) {
	w, h := ui.screen.Size()
	panelH := h - 1
	if panelH < 2 {
		return 0, 0
	}
	leftW := w / 2
	if leftW < 40 {
		leftW = w
	}
	if leftW < w {
		return w - leftW - 2, panelH - 2
	}
	return w - 2, panelH - 2
}

func (ui *UI) setToast(msg string) {
	ui.toast = msg
	ui.toastUntil = time.Now().Add(3 * time.Second)
}

func (ui *UI) copyResponseToFile() {
	entry, ok := ui.selectedEntry()
	if !ok {
		ui.setToast("no test selected")
		return
	}
	ui.statusMu.Lock()
	snapshot := append([]tests.Result(nil), ui.results...)
	ui.statusMu.Unlock()
	res := latestResult(snapshot, entry.TestID, entry.Profile)
	if res == nil {
		ui.setToast("no result yet")
		return
	}
	if err := os.MkdirAll(ui.outDir, 0o755); err != nil {
		ui.setToast("mkdir failed")
		return
	}
	name := fmt.Sprintf("copy-%s-%s-%s.txt", sanitizeFilename(entry.Profile), sanitizeFilename(entry.TestID), time.Now().Format("20060102-150405"))
	path := filepath.Join(ui.outDir, name)
	var b strings.Builder
	b.WriteString("Test: ")
	b.WriteString(res.TestID)
	b.WriteString("\n")
	b.WriteString("Profile: ")
	b.WriteString(res.Profile)
	b.WriteString("\n")
	b.WriteString("Pass: ")
	b.WriteString(fmt.Sprintf("%d", res.Pass))
	b.WriteString("\n")
	b.WriteString("Status: ")
	b.WriteString(string(res.Status))
	b.WriteString("\n")
	b.WriteString("HTTP: ")
	b.WriteString(fmt.Sprintf("%d", res.HTTPStatus))
	b.WriteString("\n")
	b.WriteString("Latency: ")
	b.WriteString(fmt.Sprintf("%d ms", res.LatencyMS))
	b.WriteString("\n\n")
	if len(tests.EffectiveTraceSteps(*res)) > 0 {
		for _, step := range tests.EffectiveTraceSteps(*res) {
			b.WriteString("Step: ")
			b.WriteString(step.Name)
			b.WriteString("\n")
			b.WriteString("Request:\n")
			b.WriteString(step.Request)
			b.WriteString("\n\n")
			b.WriteString("Response:\n")
			b.WriteString(step.Response)
			b.WriteString("\n\n")
		}
	} else {
		b.WriteString("Request:\n")
		b.WriteString(res.RequestSnippet)
		b.WriteString("\n\n")
		b.WriteString("Response:\n")
		b.WriteString(res.ResponseSnippet)
		b.WriteString("\n")
	}

	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		ui.setToast("write failed")
		return
	}
	ui.setToast("saved " + name)
}

func sanitizeFilename(s string) string {
	if s == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	out := b.String()
	out = strings.Trim(out, "._-")
	if out == "" {
		return "item"
	}
	if len(out) > 80 {
		return out[:80]
	}
	return out
}

type entry struct {
	TestID  string
	Profile string
	Label   string
	IsGroup bool
}

func (ui *UI) listEntries() []entry {
	entries := make([]entry, 0)
	for _, p := range ui.profiles {
		entries = append(entries, entry{Label: fmt.Sprintf("Model: %s", p.Name), Profile: p.Name, IsGroup: true})
		for _, t := range ui.tests {
			entries = append(entries, entry{TestID: t.ID, Profile: p.Name, Label: t.ID})
		}
	}
	return entries
}

func (ui *UI) print(x, y int, style tcell.Style, text string) {
	for i, r := range text {
		ui.screen.SetContent(x+i, y, r, nil, style)
	}
}

func buildListRows(entries []entry, width int) []listRow {
	rows := make([]listRow, 0)
	for idx, e := range entries {
		lines := wrapLines(e.Label, width)
		for i, line := range lines {
			rows = append(rows, listRow{
				Text:       line,
				EntryIndex: idx,
				IsFirst:    i == 0,
				IsGroup:    e.IsGroup,
			})
		}
	}
	return rows
}

func rowIndexForEntry(rows []listRow, entryIndex int) int {
	for i, row := range rows {
		if row.EntryIndex == entryIndex && row.IsFirst {
			return i
		}
	}
	return -1
}

func (ui *UI) printSegments(x, y, w int, segments []segment) {
	col := x
	for _, seg := range segments {
		for _, r := range seg.text {
			if col >= x+w {
				return
			}
			ui.screen.SetContent(col, y, r, nil, seg.style)
			col++
		}
	}
	for col < x+w {
		ui.screen.SetContent(col, y, ' ', nil, styleDim)
		col++
	}
}

func (ui *UI) drawProgressBar(x, y, w int, progress float64) {
	if w <= 0 {
		return
	}
	filled := int(float64(w) * progress)
	if filled < 0 {
		filled = 0
	}
	if filled > w {
		filled = w
	}
	for i := 0; i < w; i++ {
		ch := '.'
		style := styleDim
		if i < filled {
			ch = '#'
			style = stylePass
		}
		ui.screen.SetContent(x+i, y, ch, nil, style)
	}
}

func (ui *UI) fillRow(x, y, w int, style tcell.Style) {
	for i := 0; i < w; i++ {
		ui.screen.SetContent(x+i, y, ' ', nil, style)
	}
}

func (ui *UI) drawBox(x, y, w, h int, title string) {
	if w < 2 || h < 2 {
		return
	}
	for i := 0; i < w; i++ {
		ch := '-'
		if i == 0 || i == w-1 {
			ch = '+'
		}
		ui.screen.SetContent(x+i, y, ch, nil, styleBorder)
		ui.screen.SetContent(x+i, y+h-1, ch, nil, styleBorder)
	}
	for j := 1; j < h-1; j++ {
		ui.screen.SetContent(x, y+j, '|', nil, styleBorder)
		ui.screen.SetContent(x+w-1, y+j, '|', nil, styleBorder)
	}
	if title != "" {
		t := "[ " + title + " ]"
		if len(t)+2 < w {
			ui.print(x+2, y, styleTitle, t)
		}
	}
}

func (ui *UI) drawStatusGlyphs(x, y, w int, testID, profile string, passes int, selected bool) {
	if w <= 0 {
		return
	}
	ui.statusMu.Lock()
	defer ui.statusMu.Unlock()
	for i := 1; i <= passes; i++ {
		pos := x + (i-1)*2
		if pos >= x+w {
			break
		}
		status := ui.statuses[statusKey(testID, profile, i)]
		style := statusStyle(status)
		if selected {
			style = style.Background(tcell.ColorBlue)
		}
		ui.screen.SetContent(pos, y, statusGlyph(status), nil, style)
		if pos+1 < x+w {
			ui.screen.SetContent(pos+1, y, ' ', nil, styleDim)
		}
	}
}

func statusGlyph(status tests.Status) rune {
	switch status {
	case tests.StatusPass:
		return 'P'
	case tests.StatusFail:
		return 'F'
	case tests.StatusTimeout:
		return 'T'
	case tests.StatusUnsupported:
		return 'U'
	case tests.StatusRunning:
		return 'R'
	case tests.StatusQueued:
		return '.'
	default:
		return '.'
	}
}

func statusStyle(status tests.Status) tcell.Style {
	switch status {
	case tests.StatusPass:
		return stylePass
	case tests.StatusFail:
		return styleFail
	case tests.StatusTimeout:
		return styleTimeout
	case tests.StatusUnsupported:
		return styleUnsupported
	case tests.StatusRunning:
		return styleRunning
	case tests.StatusQueued:
		return styleQueued
	default:
		return styleDim
	}
}

func statusKey(testID, profile string, pass int) string {
	return fmt.Sprintf("%s|%s|%d", testID, profile, pass)
}

func pad(s string, w int) string {
	if len(s) >= w {
		return s[:w]
	}
	return s + strings.Repeat(" ", w-len(s))
}

func padRight(s string, w int) string {
	if len(s) >= w {
		return s[:w]
	}
	return s + strings.Repeat(" ", w-len(s))
}

func clipText(s string, w int) string {
	if w <= 0 || len(s) <= w {
		return s
	}
	if w <= 3 {
		return s[:w]
	}
	return s[:w-3] + "..."
}

func cutText(s string, w int) string {
	if w <= 0 || len(s) <= w {
		return s
	}
	return s[:w]
}

func wrapLines(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	rawLines := strings.Split(s, "\n")
	lines := make([]string, 0, len(rawLines))
	for _, raw := range rawLines {
		if raw == "" {
			lines = append(lines, "")
			continue
		}
		for len(raw) > width {
			lines = append(lines, raw[:width])
			raw = raw[width:]
		}
		lines = append(lines, raw)
	}
	return lines
}

func buildDetailLinesRaw(res *tests.Result) []string {
	lines := make([]string, 0)
	steps := tests.EffectiveTraceSteps(*res)
	if len(steps) > 0 {
		for _, step := range steps {
			lines = append(lines, "Step: "+step.Name)
			lines = append(lines, "Request:")
			lines = append(lines, strings.Split(step.Request, "\n")...)
			lines = append(lines, "Response (raw):")
			lines = append(lines, strings.Split(step.Response, "\n")...)
			if section, ok := extractRawSection(step.Response); ok {
				lines = append(lines, "")
				lines = append(lines, section.title)
				lines = append(lines, strings.Split(section.body, "\n")...)
			}
			lines = append(lines, "")
		}
	} else {
		lines = append(lines, "Request:")
		lines = append(lines, strings.Split(res.RequestSnippet, "\n")...)
		lines = append(lines, "Response (raw):")
		lines = append(lines, strings.Split(res.ResponseSnippet, "\n")...)
		if section, ok := extractRawSection(res.ResponseSnippet); ok {
			lines = append(lines, "")
			lines = append(lines, section.title)
			lines = append(lines, strings.Split(section.body, "\n")...)
		}
	}
	if res.ErrorMessage != "" {
		lines = append(lines, "Error: "+res.ErrorMessage)
	}
	return lines
}

func buildDetailLinesWrapped(res *tests.Result, width int) []string {
	lines := make([]string, 0)
	steps := tests.EffectiveTraceSteps(*res)
	if len(steps) > 0 {
		for _, step := range steps {
			lines = append(lines, wrapLines("Step: "+step.Name, width)...)
			lines = append(lines, "Request:")
			lines = append(lines, wrapLines(step.Request, width)...)
			lines = append(lines, "Response (raw):")
			lines = append(lines, wrapLines(step.Response, width)...)
			if section, ok := extractRawSection(step.Response); ok {
				lines = append(lines, "")
				lines = append(lines, wrapLines(section.title, width)...)
				lines = append(lines, wrapLines(section.body, width)...)
			}
			lines = append(lines, "")
		}
	} else {
		lines = append(lines, "Request:")
		lines = append(lines, wrapLines(res.RequestSnippet, width)...)
		lines = append(lines, "Response (raw):")
		lines = append(lines, wrapLines(res.ResponseSnippet, width)...)
		if section, ok := extractRawSection(res.ResponseSnippet); ok {
			lines = append(lines, "")
			lines = append(lines, wrapLines(section.title, width)...)
			lines = append(lines, wrapLines(section.body, width)...)
		}
	}
	if res.ErrorMessage != "" {
		lines = append(lines, wrapLines("Error: "+res.ErrorMessage, width)...)
	}
	return lines
}

type rawSection struct {
	title string
	body  string
}

func extractRawSection(raw string) (rawSection, bool) {
	var doc map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return rawSection{}, false
	}
	if out, ok := doc["output"]; ok {
		body := prettyJSON(out)
		if body == "" {
			return rawSection{}, false
		}
		return rawSection{title: "Output (raw):", body: body}, true
	}
	if choices, ok := doc["choices"].([]interface{}); ok && len(choices) > 0 {
		c0, _ := choices[0].(map[string]interface{})
		if c0 != nil {
			if msg, ok := c0["message"]; ok {
				body := prettyJSON(msg)
				if body == "" {
					return rawSection{}, false
				}
				return rawSection{title: "Message (raw):", body: body}, true
			}
		}
	}
	return rawSection{}, false
}

func prettyJSON(v interface{}) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}

func sliceLine(s string, offset, width int) string {
	if width <= 0 {
		return ""
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(s) {
		return ""
	}
	end := offset + width
	if end > len(s) {
		end = len(s)
	}
	return s[offset:end]
}

func latestResult(results []tests.Result, testID, profile string) *tests.Result {
	var best *tests.Result
	for _, r := range results {
		if r.TestID != testID || r.Profile != profile {
			continue
		}
		if best == nil || r.Pass > best.Pass {
			copy := r
			best = &copy
		} else if best != nil && r.Pass == best.Pass {
			copy := r
			best = &copy
		}
	}
	return best
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (ui *UI) saveSnapshot() error {
	if err := os.MkdirAll(ui.outDir, 0o755); err != nil {
		return err
	}
	name := fmt.Sprintf("snapshot-%s.md", time.Now().Format("20060102-150405"))
	path := filepath.Join(ui.outDir, name)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintf(f, "# Snapshot\n\n")
	fmt.Fprintf(f, "Generated: %s\n\n", time.Now().Format(time.RFC3339))
	ui.statusMu.Lock()
	snapshot := append([]tests.Result(nil), ui.results...)
	ui.statusMu.Unlock()
	analysis := report.Analyze(snapshot, ui.cfg)
	fmt.Fprintf(f, "## Matrix\n\n")
	profiles := make([]string, 0, len(ui.profiles))
	for _, p := range ui.profiles {
		profiles = append(profiles, p.Name)
	}
	fmt.Fprintf(f, "| Test | %s |\n", strings.Join(profiles, " | "))
	sep := make([]string, 0, len(profiles)+1)
	for i := 0; i < len(profiles)+1; i++ {
		sep = append(sep, "---")
	}
	fmt.Fprintf(f, "| %s |\n", strings.Join(sep, " | "))
	byTest := map[string]map[string][]tests.Result{}
	for _, r := range snapshot {
		if r.IsWarmup {
			continue
		}
		if byTest[r.TestID] == nil {
			byTest[r.TestID] = map[string][]tests.Result{}
		}
		byTest[r.TestID][r.Profile] = append(byTest[r.TestID][r.Profile], r)
	}
	testIDs := make([]string, 0, len(byTest))
	for id := range byTest {
		testIDs = append(testIDs, id)
	}
	sort.Strings(testIDs)
	for _, id := range testIDs {
		row := []string{id}
		for _, profile := range profiles {
			list := byTest[id][profile]
			p, fl, u, t := 0, 0, 0, 0
			for _, r := range list {
				switch r.Status {
				case tests.StatusPass:
					p++
				case tests.StatusFail:
					fl++
				case tests.StatusTimeout:
					t++
				case tests.StatusUnsupported:
					u++
				}
			}
			row = append(row, fmt.Sprintf("P%d F%d U%d T%d", p, fl, u, t))
		}
		fmt.Fprintf(f, "| %s |\n", strings.Join(row, " | "))
	}
	fmt.Fprintf(f, "## Flakiness\n\n")
	if len(analysis.Flaky) == 0 {
		fmt.Fprintln(f, "No flaky tests detected.")
	} else {
		for _, s := range analysis.Flaky {
			fmt.Fprintf(f, "- %s [%s]: %.2f (%d/%d)\n", s.TestID, s.Profile, s.PassRate, s.Passes, s.Total)
		}
	}
	return nil
}
