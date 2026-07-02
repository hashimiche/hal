package ui

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// logoArt is a product banner: a brand ANSI color (applied only on a TTY) plus
// the ASCII/Unicode art lines. Lines may use Unicode block characters — the
// reveal renderer is rune-aware, so multi-byte glyphs animate correctly.
type logoArt struct {
	color string // ANSI SGR color sequence, "" for none
	lines []string
}

// Brand colors (256-color SGR) approximating each product's identity.
const (
	colorVault     = "\033[38;5;221m" // gold/yellow
	colorTerraform = "\033[38;5;99m"  // purple
	colorNomad     = "\033[38;5;42m"  // green (Nomad ~#00CA8E)
	colorBoundary  = "\033[38;5;203m" // coral red (Boundary ~#F24C53)
	colorConsul    = "\033[38;5;205m" // pink (Consul ~#DC477D)
	colorObs       = "\033[38;5;208m" // orange (observability / Grafana ~#F46800)
	colorPlus      = "\033[38;5;45m"  // cyan (HAL+)
	colorReset     = "\033[0m"
)

// productLogos maps a product key (the `hal <product>` name) to its banner.
var productLogos = map[string]logoArt{
	"vault": {
		color: colorVault,
		lines: []string{
			"::::::::::::::::::::::::::::::::::",
			" :::::::::::::::::::::::::::::::: ",
			"  ::::::::::::::::::::::::::::::  ",
			"   ::::::::::::::::::::::::::::   ",
			"    ::::::::  ::  ::  ::::::::    ",
			"     ::::::::::::::::::::::::     ",
			"      ::::::  ::  ::  ::::::      ",
			"       ::::::::::::::::::::       ",
			"        ::::::::  ::::::::        ",
			"         ::::::::::::::::         ",
			"          ::::::::::::::          ",
			"           ::::::::::::           ",
			"            ::::::::::            ",
			"             ::::::::             ",
			"              ::::::              ",
			"               ::::               ",
			"                ::                ",
		},
	},
	// Reused from the original tf api build animation so there is a single home
	// for the Terraform mark.
	"terraform": {
		color: colorTerraform,
		lines: []string{
			"   ***                          ",
			"   ******.                      ",
			"   ********..                .  ",
			"   ********..***          .###  ",
			"   ********..******.   .######  ",
			"      *****..******** ########  ",
			"         **..******** ########  ",
			"            .******** ########  ",
			"            .* .***** #####.    ",
			"            .****. ** ##        ",
			"            .*******            ",
			"            .********           ",
			"            .********           ",
			"             .*******           ",
			"                 ****           ",
			"                    *           ",
		},
	},
	"nomad": {
		color: colorNomad,
		lines: []string{
			"              ++++              ",
			"           ++++++++++           ",
			"       ++++++++++++++++++       ",
			"    ++++++++++++++++++++++++    ",
			" +++++++++++++++++++++ ++++++++ ",
			"+++++++++++++++++++    +++++++++",
			"++++++++++++++++++     +++++++++",
			"++++++++++++ +++++     +++++++++",
			"+++++++++        +     +++++++++",
			"+++++++++              +++++++++",
			"+++++++++     +        +++++++++",
			"+++++++++     ++++ +++++++++++++",
			"+++++++++     ++++++++++++++++++",
			"+++++++++    +++++++++++++++++++",
			" ++++++++ +++++++++++++++++++++ ",
			"    ++++++++++++++++++++++++    ",
			"       ++++++++++++++++++       ",
			"           ++++++++++           ",
			"              ++++              ",
		},
	},
	"boundary": {
		color: colorBoundary,
		lines: []string{
			"++++++++++++++++++++++++    ",
			"+++++++++++++++++++++++++   ",
			"++++++++++++++++++++++++++  ",
			"+++++++++++++++++++++++++++ ",
			"+++++++            ++++++++ ",
			"+++++++           ++++++++  ",
			"+++++++          ++++++++   ",
			"+++++++         ++++++++    ",
			"+++++++        ++++++++     ",
			"+++++++       ++++++++      ",
			"+++++++        ++++++++     ",
			"+++++++         ++++++++    ",
			"                 ++++++++   ",
			"                  ++++++++  ",
			"                   ++++++++.",
			"   ++- ++++++++++++++++++++=",
			"          ++++++++++++++++: ",
			"+++ +++++++++++++++++++++.  ",
			"+++ ++++++++++++++++++++    ",
		},
	},
	"consul": {
		color: colorConsul,
		lines: []string{
			"          **********          ",
			"      +*****************       ",
			"    *********************      ",
			"   *******          ***        ",
			"  ******                  **   ",
			" *****.                        ",
			"******       +***       **  ** ",
			"*****       ******            ",
			"*****       ******            ",
			"******       +***       **  ** ",
			" *****.                        ",
			"  ******                  **   ",
			"   *******          ***        ",
			"    *********************      ",
			"      +*****************       ",
			"          **********          ",
		},
	},
	"observability": {
		color: colorObs,
		lines: []string{
			"   .@@@@@@@@@@@@@@@@@@@@@@@@@@@@@       ",
			"   @@                          @@.      ",
			"   @@         @                @@.      ",
			"   @@    .@  .@@  @@     @@@@@. @.      ",
			"   @@    @@. @@@ .@@: @@@@@@@@@@@.      ",
			"   @@ ..@..@ @ @-@. .@@@       @@@@     ",
			"   @@      @.@ .@@  @@@         .@@     ",
			"   @@      .@@  @@  @@.          @@@    ",
			"   @@       @.      @@.          @@.    ",
			"   @@               %@@.       .@@@     ",
			"   @@@@@@@@@@@@@@@@@@.@@@@...@@@@@      ",
			"   :@@@@@@@@@@@@@@@@@@:.@@@@@@@@@@      ",
			"               @@@@@@         @@@@@     ",
			"           .@@@@@@@@@@@@.      @@@@@    ",
			"           @@@@@@@@@@@@@@.      @@@@@   ",
			"                                 @@@@   ",
		},
	},
	"plus": {
		color: colorPlus,
		lines: []string{
			"  _    _              _              ",
			" | |  | |     /\\     | |         _   ",
			" | |__| |    /  \\    | |       _| |_ ",
			" |  __  |   / /\\ \\   | |      |_   _|",
			" | |  | |  / ____ \\  | |____    |_|  ",
			" |_|  |_| /_/    \\_\\ |______|        ",
		},
	},
}

// HasLogo reports whether a banner exists for the given product key.
func HasLogo(product string) bool {
	_, ok := productLogos[strings.ToLower(strings.TrimSpace(product))]
	return ok
}

// Logo prints the product's ASCII banner. On an interactive terminal (and
// without --verbose) it reveals the art left-to-right like an installer splash;
// otherwise it prints the art statically so logs and MCP transports stay clean.
func Logo(product string) {
	art, ok := productLogos[strings.ToLower(strings.TrimSpace(product))]
	if !ok {
		return
	}

	printLogoTopMargin()

	if !isTTY() || Verbose {
		for _, line := range art.lines {
			fmt.Println(strings.TrimRight(line, " "))
		}
		fmt.Println()
		return
	}

	maxCols := maxRuneLen(art.lines)
	// Aim for a quick ~0.6s reveal regardless of width.
	stepCols := maxCols/30 + 1
	delay := 18 * time.Millisecond

	printed := 0
	for reveal := 0; reveal < maxCols; reveal += stepCols {
		frame, n := renderLogoFrame(art, reveal, maxCols)
		redraw(frame, &printed, n)
		time.Sleep(delay)
	}
	// Final full frame.
	frame, n := renderLogoFrame(art, maxCols, maxCols)
	redraw(frame, &printed, n)
}

// logoTopMargin is the number of blank lines printed above a banner so it does
// not hug the shell prompt.
const logoTopMargin = 1

// printLogoTopMargin prints the leading blank lines above a banner. These lines
// sit above the redrawn frame block, so the cursor-up redraw logic never
// overwrites them.
func printLogoTopMargin() {
	for i := 0; i < logoTopMargin; i++ {
		fmt.Println()
	}
}

// redraw moves the cursor up over the previously printed block (if any) and
// prints the new frame, tracking how many lines it occupied.
func redraw(frame string, printed *int, lines int) {
	if *printed > 0 {
		fmt.Printf("\033[%dA", *printed)
	}
	fmt.Print(frame)
	*printed = lines
}

// renderLogoFrame returns the banner revealed up to revealCols columns and the
// number of terminal lines it occupies. Slicing is rune-aware.
func renderLogoFrame(art logoArt, revealCols, maxCols int) (string, int) {
	if maxCols <= 0 {
		maxCols = 1
	}
	if revealCols < 0 {
		revealCols = 0
	}
	if revealCols > maxCols {
		revealCols = maxCols
	}

	var b strings.Builder
	for _, line := range art.lines {
		runes := []rune(line)
		limit := revealCols
		if limit > len(runes) {
			limit = len(runes)
		}
		if art.color != "" {
			b.WriteString(art.color)
		}
		b.WriteString(string(runes[:limit]))
		if art.color != "" {
			b.WriteString(colorReset)
		}
		// Pad the rest of the row so cursor-up redraws overwrite cleanly.
		if limit < maxCols {
			b.WriteString(strings.Repeat(" ", maxCols-limit))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	return b.String(), len(art.lines) + 1
}

// maxRuneLen returns the widest line measured in runes.
func maxRuneLen(lines []string) int {
	maxLen := 0
	for _, line := range lines {
		if n := len([]rune(line)); n > maxLen {
			maxLen = n
		}
	}
	return maxLen
}

// ─── Logo loader ────────────────────────────────────────────────────────────────
// The logo loader shares the same loading system as the feature spinner: a top
// line shows the spinning icon and the current activity (updated in place as the
// flow advances), exactly like ui.Start/Step/Stop. Beneath it, the product
// banner reveals left-to-right (tf-api style) at a steady pace — the "fancy"
// part unique to create flows. This replaces ad-hoc per-step status lines
// (🚀 Deploying…, ⚙️ Preparing…, ⏳ Waiting…) on a create command.

// logoTick drives both the spinner icon and the one-column-per-tick reveal. It
// matches the feature spinner cadence so the loading icon feels identical.
const logoTick = 90 * time.Millisecond

type logoLoader struct {
	mu      sync.Mutex
	art     logoArt
	maxCols int
	reveal  int // revealed columns, advanced one per tick
	caption string
	frame   int // spinner frame index
	ceiling int // column ceiling the reveal advances toward; <0 means free-run
	// creep slowly raises the ceiling during a long step so the reveal never
	// fully stalls. creepEvery == 0 disables it.
	creepEvery time.Duration
	creepCap   int
	lastCreep  time.Time
	printed    int
	animated   bool
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

// currentLoader holds the in-flight logo loader, mirroring the spinner handle so
// nested helpers can drive steps via package-level functions.
var currentLoader *logoLoader

// LogoStart begins a logo-as-progress flow for the product. On an interactive
// terminal (and without --verbose) the banner reveals at a steady pace while a
// header shows the current activity and percentage; otherwise it prints the
// static banner once and LogoStep emits plain one-line-per-step output. The
// variadic int argument is accepted for backward compatibility and ignored.
func LogoStart(product string, _ ...int) {
	key := strings.ToLower(strings.TrimSpace(product))

	art, ok := productLogos[key]
	l := &logoLoader{
		art:      art,
		animated: ok && isTTY() && !Verbose,
		ceiling:  -1, // free-run until a caller steers progress in columns
		stopCh:   make(chan struct{}),
	}
	currentLoader = l

	if !l.animated {
		// Static brand banner once (no-op if the product has no logo), then
		// LogoStep falls back to plain lines.
		if ok {
			Logo(product)
		}
		return
	}

	l.maxCols = maxRuneLen(art.lines)
	printLogoTopMargin()
	l.wg.Add(1)
	go l.run()
}

// LogoStep updates the header caption to the current activity. In animated mode
// the steady reveal continues underneath; otherwise it prints a plain step line.
func LogoStep(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l := currentLoader
	if l == nil || !l.animated {
		fmt.Printf("  → %s\n", msg)
		return
	}
	l.mu.Lock()
	l.caption = msg
	l.mu.Unlock()
}

// LogoAdvance raises the reveal ceiling by cols columns — use it to map a short
// step to a fixed amount of logo fill (typically 1–2 columns). It cancels any
// active creep, since a new discrete step has started. The reveal animates
// smoothly up to the new ceiling.
func LogoAdvance(cols int) {
	l := currentLoader
	if l == nil || !l.animated {
		return
	}
	l.mu.Lock()
	if l.ceiling < 0 {
		l.ceiling = l.reveal // leave free-run, anchor at current fill
	}
	l.ceiling += cols
	if l.ceiling > l.maxCols {
		l.ceiling = l.maxCols
	}
	l.creepEvery = 0
	l.mu.Unlock()
}

// LogoCreep makes the reveal advance by one column every interval, stopping a
// couple of columns short of full, so a long-running step keeps inching forward
// instead of stalling. The next LogoAdvance or LogoStop cancels it.
func LogoCreep(every time.Duration) {
	l := currentLoader
	if l == nil || !l.animated {
		return
	}
	l.mu.Lock()
	if l.ceiling < 0 {
		l.ceiling = l.reveal
	}
	l.creepEvery = every
	l.creepCap = l.maxCols - 2 // leave room for the remaining steps + final snap
	if l.creepCap < l.ceiling {
		l.creepCap = l.ceiling
	}
	l.lastCreep = time.Now()
	l.mu.Unlock()
}

// LogoStop completes the reveal (snapping the banner to 100%), clears the
// header, and ends the flow. It is safe to call multiple times.
func LogoStop() {
	l := currentLoader
	currentLoader = nil
	if l == nil || !l.animated {
		return
	}
	close(l.stopCh)
	l.wg.Wait()

	// Snap to a full, header-free banner so a summary can print cleanly below.
	// The final frame has fewer lines than the running frame (no spinner header),
	// so clear from the cursor to end-of-screen to remove any stale lines.
	frame, n := renderLogoFrame(l.art, l.maxCols, l.maxCols)
	if l.printed > 0 {
		fmt.Printf("\033[%dA", l.printed)
	}
	fmt.Print("\033[J") // clear from cursor to end of screen
	fmt.Print(frame)
	l.printed = n
}

func (l *logoLoader) run() {
	defer l.wg.Done()
	ticker := time.NewTicker(logoTick)
	defer ticker.Stop()
	for {
		select {
		case <-l.stopCh:
			return
		case <-ticker.C:
			l.mu.Lock()
			l.frame++
			// Creep: during a long step, slowly raise the ceiling so the reveal
			// keeps inching forward instead of stalling.
			if l.creepEvery > 0 && l.ceiling < l.creepCap && time.Since(l.lastCreep) >= l.creepEvery {
				l.ceiling++
				l.lastCreep = time.Now()
			}
			// Determine the reveal ceiling: free-run to full, or the column
			// ceiling set by LogoAdvance/LogoCreep.
			ceiling := l.maxCols
			if l.ceiling >= 0 {
				ceiling = l.ceiling
			}
			if ceiling > l.maxCols {
				ceiling = l.maxCols
			}
			if l.reveal < ceiling {
				l.reveal++
			}
			frame, n := l.render()
			printed := l.printed
			l.printed = n
			l.mu.Unlock()

			if printed > 0 {
				fmt.Printf("\033[%dA", printed)
			}
			fmt.Print(frame)
		}
	}
}

// render builds the spinner header (loading icon + current activity) and the
// partially revealed banner beneath it. Caller must hold l.mu.
func (l *logoLoader) render() (string, int) {
	reveal := l.reveal
	if reveal > l.maxCols {
		reveal = l.maxCols
	}

	var b strings.Builder
	// Top line: same format as the feature spinner — "  <icon> <activity>".
	sp := spinnerFrames[l.frame%len(spinnerFrames)]
	header := fmt.Sprintf("  %s %s", sp, l.caption)
	b.WriteString(padRunes(header, logoCaptionWidth))
	b.WriteByte('\n')
	b.WriteByte('\n')

	for _, line := range l.art.lines {
		runes := []rune(line)
		limit := reveal
		if limit > len(runes) {
			limit = len(runes)
		}
		if l.art.color != "" {
			b.WriteString(l.art.color)
		}
		b.WriteString(string(runes[:limit]))
		if l.art.color != "" {
			b.WriteString(colorReset)
		}
		if limit < l.maxCols {
			b.WriteString(strings.Repeat(" ", l.maxCols-limit))
		}
		b.WriteByte('\n')
	}

	return b.String(), len(l.art.lines) + 2
}

const logoCaptionWidth = 60

// padRunes pads s with trailing spaces to at least width runes so a redraw
// fully overwrites a longer previous caption.
func padRunes(s string, width int) string {
	if n := len([]rune(s)); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}
